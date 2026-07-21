/*
2026 © Postgres.ai
*/

package msgproc

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/pkg/errors"

	"gitlab.com/postgres-ai/database-lab/v3/pkg/client/dblabapi/types"
	"gitlab.com/postgres-ai/database-lab/v3/pkg/log"

	"gitlab.com/postgres-ai/joe/pkg/bot/querier"
	pgaiv2sdk "gitlab.com/postgres-ai/joe/pkg/pgai_v2_sdk"
	"gitlab.com/postgres-ai/joe/pkg/services/usermanager"
	"gitlab.com/postgres-ai/joe/pkg/transmission/pgtransmission"
)

// The FIXED command_string envelopes composed by the platform dispatcher
// (public.joe_command_dispatch). Joe peels them to derive the companion
// text-format EXPLAIN; the envelopes themselves run verbatim.
const (
	v2PlanEnvelopePrefix    = "explain (format json, costs on, buffers off, analyze off, timing off) "
	v2ExplainEnvelopePrefix = "begin; explain (analyze on, format json, costs on, buffers on, timing on) "
	v2ExplainEnvelopeSuffix = "; rollback;"

	v2ExplainJSONQuery = "explain (analyze on, format json, costs on, buffers on, timing on) "
	v2ExplainTextQuery = "explain (analyze on, costs on, buffers on, timing on) "
	v2PlanTextQuery    = "explain (costs on) "
	v2HypoPlanQuery    = "explain (format json, costs on) "
)

// Result caps (the platform additionally minimizes at read time).
const (
	v2ResultRowsCap   = 1000
	v2ActivityRowsCap = 200
	// v2NoticesCap bounds the notices captured per exec command: a statement
	// can RAISE arbitrarily many, and the capture must not be an unbounded
	// memory path.
	v2NoticesCap = 1000
)

// v2UserPrefix and v2ClonePrefix namespace per-session v2 resources.
const (
	v2UserPrefix  = "v2_session_"
	v2ClonePrefix = "v2-"
)

// v2TerminatePIDRe extracts the backend pid from the dispatcher-composed
// terminate command string.
var v2TerminatePIDRe = regexp.MustCompile(`pg_terminate_backend\((\d+)\)`)

// v2CloneID builds the Database Lab clone ID for a platform session.
func v2CloneID(sessionID string) string {
	return v2ClonePrefix + sessionID
}

// ExecuteV2Command executes a Joe API v2 dispatch request on the session's
// Database Lab clone and returns the per-command reply result fields.
// Commands of one platform session are serialized; distinct sessions run
// concurrently on their own clones.
func (s *ProcessingService) ExecuteV2Command(ctx context.Context,
	req *pgaiv2sdk.DispatchRequest) (map[string]interface{}, error) {
	// Resolve the runner up front: an unsupported command must not create
	// a clone session.
	runner, err := s.v2Runner(req.Command)
	if err != nil {
		return nil, err
	}

	unlock, err := s.lockV2Session(ctx, req.SessionID)
	if err != nil {
		return nil, err
	}
	defer unlock()

	user, err := s.UserManager.CreateUser(v2UserPrefix + req.SessionID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to prepare the v2 session user")
	}

	if err := s.ensureV2Session(ctx, user, req.SessionID); err != nil {
		return nil, errors.Wrap(err, "failed to prepare a Database Lab clone session")
	}

	user.Session.LastActionTs = time.Now()

	return runner(ctx, user, req)
}

// v2CommandRunner executes one dispatched v2 command on a prepared session.
type v2CommandRunner func(ctx context.Context, user *usermanager.User,
	req *pgaiv2sdk.DispatchRequest) (map[string]interface{}, error)

// v2Runner routes a dispatch command to its runner. It must cover every
// command the dispatch validator (pgaiv2sdk.IsSupportedCommand) accepts.
func (s *ProcessingService) v2Runner(command string) (v2CommandRunner, error) {
	switch strings.ToLower(command) {
	case pgaiv2sdk.CommandPlan:
		return func(ctx context.Context, user *usermanager.User,
			req *pgaiv2sdk.DispatchRequest) (map[string]interface{}, error) {
			return s.runV2Plan(ctx, user, req.CommandString)
		}, nil

	case pgaiv2sdk.CommandExplain:
		return func(ctx context.Context, user *usermanager.User,
			req *pgaiv2sdk.DispatchRequest) (map[string]interface{}, error) {
			return s.runV2Explain(ctx, user, req.CommandString)
		}, nil

	case pgaiv2sdk.CommandExec:
		return func(ctx context.Context, user *usermanager.User,
			req *pgaiv2sdk.DispatchRequest) (map[string]interface{}, error) {
			return s.runV2Exec(ctx, user, req.CommandString)
		}, nil

	case pgaiv2sdk.CommandHypo:
		return func(ctx context.Context, user *usermanager.User,
			req *pgaiv2sdk.DispatchRequest) (map[string]interface{}, error) {
			return s.runV2Hypo(ctx, user, req.CommandString, req.Args["query"])
		}, nil

	case pgaiv2sdk.CommandActivity:
		return func(ctx context.Context, user *usermanager.User,
			req *pgaiv2sdk.DispatchRequest) (map[string]interface{}, error) {
			return s.runV2Activity(ctx, user, req.CommandString)
		}, nil

	case pgaiv2sdk.CommandDescribe:
		return func(ctx context.Context, user *usermanager.User,
			req *pgaiv2sdk.DispatchRequest) (map[string]interface{}, error) {
			return s.runV2Describe(ctx, user, req.CommandString)
		}, nil

	case pgaiv2sdk.CommandTerminate:
		return func(ctx context.Context, user *usermanager.User,
			req *pgaiv2sdk.DispatchRequest) (map[string]interface{}, error) {
			return s.runV2Terminate(ctx, user, req.CommandString)
		}, nil

	case pgaiv2sdk.CommandReset:
		return func(ctx context.Context, user *usermanager.User,
			_ *pgaiv2sdk.DispatchRequest) (map[string]interface{}, error) {
			return s.runV2Reset(ctx, user)
		}, nil
	}

	return nil, errors.Errorf("unsupported v2 command %q", command)
}

// lockV2Session serializes command execution within one platform session.
// The wait is cancellable (H1): a session wedged by a hung command must not
// leak a goroutine per queued dispatch until process restart.
func (s *ProcessingService) lockV2Session(ctx context.Context, sessionID string) (func(), error) {
	sem := s.v2SessionSemaphore(sessionID)

	select {
	case sem <- struct{}{}:
		return func() { <-sem }, nil
	case <-ctx.Done():
		return nil, errors.Wrapf(ctx.Err(), "gave up waiting for session %q (busy)", sessionID)
	}
}

// tryLockV2Session acquires the session lock only when it is free — the
// reaper uses it so idle cleanup can never race a running command (H2).
func (s *ProcessingService) tryLockV2Session(sessionID string) (func(), bool) {
	sem := s.v2SessionSemaphore(sessionID)

	select {
	case sem <- struct{}{}:
		return func() { <-sem }, true
	default:
		return nil, false
	}
}

// v2SessionSemaphore resolves the per-session binary semaphore.
func (s *ProcessingService) v2SessionSemaphore(sessionID string) chan struct{} {
	semIface, _ := s.v2SessionLocks.LoadOrStore(sessionID, make(chan struct{}, 1))

	return semIface.(chan struct{})
}

// ensureV2Session provides the user with a running clone session, creating a
// per-platform-session Database Lab clone on first use. It deliberately
// avoids the v1 messenger notifications: v2 progress is tracked by the
// platform's command lifecycle, not chat messages.
func (s *ProcessingService) ensureV2Session(ctx context.Context, user *usermanager.User, sessionID string) error {
	if user.Session.Clone != nil {
		// Pool == nil happens after a process restart (connections are not
		// persisted): fall through to the full rebuild below.
		if user.Session.Pool != nil && s.isActiveSession(ctx, user.Session.Clone.ID) {
			if conn := user.Session.CloneConnection; conn != nil && conn.Ping(ctx) == nil {
				return nil
			}

			// The clone is up but the cached connection died (e.g. its
			// backend was terminated); close the dead connection and
			// re-acquire a validated fresh one. On any failure fall
			// through to the full session rebuild below.
			if conn := user.Session.CloneConnection; conn != nil {
				if err := conn.Close(ctx); err != nil {
					log.Dbg("v2: failed to close the dead clone connection:", err)
				}

				user.Session.CloneConnection = nil
			}

			// Release the dead connection's pool wrapper so the slot is
			// reclaimed before re-acquiring (M2).
			if wrapper := user.Session.ClonePoolConn; wrapper != nil {
				wrapper.Release()
				user.Session.ClonePoolConn = nil
			}

			if cloneConn, err := user.Session.Pool.Acquire(ctx); err == nil {
				if cloneConn.Conn().Ping(ctx) == nil {
					user.Session.ClonePoolConn = cloneConn
					user.Session.CloneConnection = cloneConn.Conn()

					return nil
				}

				// The pool handed out another dead connection: return it
				// closed (the pool destroys it) and rebuild from scratch.
				if err := cloneConn.Conn().Close(ctx); err != nil {
					log.Dbg("v2: failed to close the re-acquired dead connection:", err)
				}

				cloneConn.Release()
			}
		}

		// Unreachable or inactive clone: rebuild the session from scratch.
		if err := s.destroySession(ctx, user); err != nil {
			log.Dbg("v2: failed to destroy the stale session clone:", err)
			s.stopSession(ctx, user)
		}
	}

	// Unlike the v1 flow the clone user is NOT restricted: the hypo command
	// needs `create extension if not exists hypopg` on a fresh clone
	// (superuser-only), and the platform's execution policy/scopes — not
	// clone-user privileges — are the v2 authorization boundary (v1 exec
	// runs arbitrary SQL either way).
	clone, err := s.createDBLabClone(ctx, user, v2CloneID(sessionID), false)
	if err != nil {
		// Clone IDs are deterministic per session, so a stale clone left by
		// an earlier partial setup blocks re-creation forever ("clone with
		// such ID already exists"). It is ours by construction: destroy it
		// and retry once (M1).
		if delErr := s.DBLab.DestroyClone(ctx, v2CloneID(sessionID)); delErr != nil {
			log.Dbg("v2: failed to destroy a possible stale clone:", delErr)
		} else {
			clone, err = s.createDBLabClone(ctx, user, v2CloneID(sessionID), false)
		}

		if err != nil {
			return errors.Wrap(err, "failed to create a Database Lab clone")
		}
	}

	dblabClone := s.buildDBLabCloneConn(clone.DB)

	pool, userConn, err := initConn(ctx, dblabClone)
	if err != nil {
		// Destroy the just-created clone: leaving it alive would wedge every
		// retry on the deterministic clone ID and leak the clone (M1).
		if delErr := s.DBLab.DestroyClone(ctx, clone.ID); delErr != nil {
			log.Dbg("v2: failed to destroy the clone after a connection failure:", delErr)
		}

		return errors.Wrap(err, "failed to init database connection")
	}

	user.Session.ConnParams = dblabClone
	user.Session.Clone = clone
	user.Session.Pool = pool
	user.Session.ClonePoolConn = userConn
	user.Session.CloneConnection = userConn.Conn()
	user.Session.PlatformSessionID = sessionID
	user.Session.Direct = true
	user.Session.LastActionTs = time.Now()

	return nil
}

// runV2Plan executes the fixed EXPLAIN (FORMAT JSON, no ANALYZE) envelope and
// a companion text EXPLAIN of the bare statement, inside an explicitly
// read-only, rolled-back transaction (SB3: plan never executes the statement,
// so read-only is safe and blocks any write escape).
func (s *ProcessingService) runV2Plan(ctx context.Context, user *usermanager.User,
	commandString string) (map[string]interface{}, error) {
	sql := strings.TrimPrefix(commandString, v2PlanEnvelopePrefix)

	if err := v2EnsureSingleStatement(sql); err != nil {
		return nil, err
	}

	var planJSON, planText string

	err := s.inRolledBackV2Tx(ctx, user, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		var txErr error

		if planJSON, txErr = queryV2TextLines(ctx, tx, commandString); txErr != nil {
			return txErr
		}

		planText, txErr = queryV2TextLines(ctx, tx, v2PlanTextQuery+sql)

		return txErr
	})
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"plan_text": planText,
		"plan_json": json.RawMessage(planJSON),
	}, nil
}

// runV2Explain executes EXPLAIN ANALYZE inside an explicitly rolled-back
// transaction (M5: a data-modifying statement must never commit a write on
// the clone), producing both the JSON and the text form.
func (s *ProcessingService) runV2Explain(ctx context.Context, user *usermanager.User,
	commandString string) (map[string]interface{}, error) {
	sql := strings.TrimPrefix(commandString, v2ExplainEnvelopePrefix)
	sql = strings.TrimSuffix(sql, v2ExplainEnvelopeSuffix)

	if err := v2EnsureSingleStatement(sql); err != nil {
		return nil, err
	}

	var planJSON, planText string

	// NOT AccessMode ReadOnly: EXPLAIN ANALYZE actually executes the
	// statement, and explaining DML (UPDATE/DELETE/INSERT) on the clone is a
	// core Joe feature — a read-only transaction would reject it. The
	// no-commit invariant is upheld by the unconditional rollback plus the
	// single-statement guard above (no `commit;` escape).
	err := s.inRolledBackV2Tx(ctx, user, pgx.TxOptions{}, func(tx pgx.Tx) error {
		var txErr error

		if planJSON, txErr = queryV2TextLines(ctx, tx, v2ExplainJSONQuery+sql); txErr != nil {
			return txErr
		}

		planText, txErr = queryV2TextLines(ctx, tx, v2ExplainTextQuery+sql)

		return txErr
	})
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"plan_text": planText,
		"plan_json": json.RawMessage(planJSON),
	}, nil
}

// runV2Exec executes the statement(s) on the session's persistent clone
// connection, streaming the last result set (capped at v2ResultRowsCap) plus
// the row count and notices.
func (s *ProcessingService) runV2Exec(ctx context.Context, user *usermanager.User,
	commandString string) (map[string]interface{}, error) {
	pgConn := user.Session.CloneConnection.PgConn()

	v2Notices.start(pgConn)

	resultRows, rowCount, err := collectV2ExecResult(
		&pgconnResultStream{mrr: pgConn.Exec(ctx, commandString)}, v2ResultRowsCap)

	notices := v2Notices.stop(pgConn)
	if notices == nil {
		notices = []string{}
	}

	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"result_rows": resultRows,
		"row_count":   json.Number(strconv.Itoa(rowCount)),
		"notices":     stringsToInterfaces(notices),
	}, nil
}

// v2RowStream is one result set's row stream (*pgconn.ResultReader).
type v2RowStream interface {
	NextRow() bool
	FieldDescriptions() []pgconn.FieldDescription
	Values() [][]byte
	Close() (pgconn.CommandTag, error)
}

// v2ResultStream is a multi-statement result stream
// (*pgconn.MultiResultReader via pgconnResultStream).
type v2ResultStream interface {
	NextResult() bool
	ResultReader() v2RowStream
	Close() error
}

// pgconnResultStream adapts *pgconn.MultiResultReader to v2ResultStream.
type pgconnResultStream struct {
	mrr *pgconn.MultiResultReader
}

func (s *pgconnResultStream) NextResult() bool { return s.mrr.NextResult() }

func (s *pgconnResultStream) ResultReader() v2RowStream { return s.mrr.ResultReader() }

func (s *pgconnResultStream) Close() error { return s.mrr.Close() }

// collectV2ExecResult streams every result set of a (possibly
// multi-statement) exec, keeping only the LAST result set's data: up to
// limit converted rows plus the total row count. Rows beyond the cap are
// drained and counted but never retained, so an oversized result cannot
// materialize in memory (SB2: `select * from big_table` must not OOM Joe).
// Values are converted row-by-row because the reader's raw values live in a
// reused wire buffer.
func collectV2ExecResult(results v2ResultStream, limit int) ([]interface{}, int, error) {
	resultRows := []interface{}{}
	rowCount := 0

	for results.NextResult() {
		reader := results.ResultReader()
		fields := reader.FieldDescriptions()

		rows := []interface{}{}
		count := 0

		for reader.NextRow() {
			count++

			if len(rows) >= limit {
				// Drain to keep the count accurate; never retain the row.
				continue
			}

			rawValues := reader.Values()
			row := make(map[string]interface{}, len(fields))

			for i, field := range fields {
				var raw []byte
				if i < len(rawValues) {
					raw = rawValues[i]
				}

				row[field.Name] = convertV2Value(field.DataTypeOID, raw)
			}

			rows = append(rows, row)
		}

		commandTag, err := reader.Close()
		if err != nil {
			_ = results.Close()

			return nil, 0, err
		}

		// The last result set wins — mirroring the pre-streaming semantics.
		if len(fields) > 0 {
			resultRows, rowCount = rows, count
		} else {
			resultRows = []interface{}{}
			rowCount = int(commandTag.RowsAffected())
		}
	}

	if err := results.Close(); err != nil {
		return nil, 0, err
	}

	return resultRows, rowCount, nil
}

// runV2Hypo creates the hypothetical index (rolled back afterwards) and
// EXPLAINs the typed target query against it.
func (s *ProcessingService) runV2Hypo(ctx context.Context, user *usermanager.User,
	commandString, targetQuery string) (map[string]interface{}, error) {
	if targetQuery == "" {
		return nil, errors.New("hypo dispatch carried no args.query")
	}

	if err := v2EnsureSingleStatement(commandString); err != nil {
		return nil, err
	}

	if err := v2EnsureSingleStatement(targetQuery); err != nil {
		return nil, err
	}

	var (
		hypoPlan  string
		hypoNames []string
	)

	// NOT AccessMode ReadOnly: `create extension if not exists hypopg` and
	// hypopg_create_index need a writable transaction; everything is rolled
	// back and the single-statement guard blocks any `commit;` escape.
	err := s.inRolledBackV2Tx(ctx, user, pgx.TxOptions{}, func(tx pgx.Tx) error {
		if _, txErr := tx.Exec(ctx, "create extension if not exists hypopg"); txErr != nil {
			return errors.Wrap(txErr, "failed to init the HypoPG extension")
		}

		rows, txErr := tx.Query(ctx, "select indexname from hypopg_create_index($1)", commandString)
		if txErr != nil {
			return errors.Wrap(txErr, "failed to create a hypothetical index")
		}

		for rows.Next() {
			var name string
			if txErr := rows.Scan(&name); txErr != nil {
				rows.Close()
				return txErr
			}

			hypoNames = append(hypoNames, name)
		}

		rows.Close()

		if txErr := rows.Err(); txErr != nil {
			return txErr
		}

		hypoPlan, txErr = queryV2TextLines(ctx, tx, v2HypoPlanQuery+targetQuery)

		return txErr
	})
	if err != nil {
		return nil, err
	}

	hypoUsed := false

	for _, name := range hypoNames {
		if strings.Contains(hypoPlan, name) {
			hypoUsed = true
			break
		}
	}

	return map[string]interface{}{
		"hypo_plan": json.RawMessage(hypoPlan),
		"hypo_used": hypoUsed,
	}, nil
}

// runV2Activity snapshots the dispatcher-composed pg_stat_activity query.
func (s *ProcessingService) runV2Activity(ctx context.Context, user *usermanager.User,
	commandString string) (map[string]interface{}, error) {
	snapshot, err := queryV2Snapshot(ctx, user.Session.Pool, commandString, v2ActivityRowsCap)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{"snapshot": snapshot}, nil
}

// runV2Describe transmits the psql meta-command through the existing psql
// transmission runner. The context bounds the psql process (H1): a hung
// psql must not hold the session lock forever.
func (s *ProcessingService) runV2Describe(ctx context.Context, user *usermanager.User,
	commandString string) (map[string]interface{}, error) {
	runner := pgtransmission.NewPgTransmitter(user.Session.ConnParams, pgtransmission.LogsEnabledDefault)

	output, err := runner.RunWithContext(ctx, commandString)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"snapshot": map[string]interface{}{
			"describe": commandString,
			"output":   output,
		},
	}, nil
}

// runV2Terminate runs the dispatcher-composed pg_terminate_backend call.
func (s *ProcessingService) runV2Terminate(ctx context.Context, user *usermanager.User,
	commandString string) (map[string]interface{}, error) {
	var terminated bool
	if err := user.Session.Pool.QueryRow(ctx, commandString).Scan(&terminated); err != nil {
		return nil, err
	}

	result := map[string]interface{}{
		"terminated": terminated,
		"pid":        nil,
	}

	if pid := extractV2TerminatePID(commandString); pid != "" {
		result["pid"] = json.Number(pid)
	}

	return result, nil
}

// runV2Reset resets the session's clone to the latest snapshot and
// re-establishes the database connections (the clone's Postgres restarts).
func (s *ProcessingService) runV2Reset(ctx context.Context, user *usermanager.User) (map[string]interface{}, error) {
	if err := s.DBLab.ResetClone(ctx, user.Session.Clone.ID, types.ResetCloneRequest{Latest: true}); err != nil {
		return nil, errors.Wrap(err, "failed to reset clone")
	}

	if user.Session.CloneConnection != nil {
		if err := user.Session.CloneConnection.Close(ctx); err != nil {
			log.Dbg("failed to close user connection after reset:", err)
		}

		user.Session.CloneConnection = nil
	}

	// Release the retired connection's pool wrapper so its slot is
	// reclaimed before re-acquiring (M2).
	if wrapper := user.Session.ClonePoolConn; wrapper != nil {
		wrapper.Release()
		user.Session.ClonePoolConn = nil
	}

	for _, idleConnection := range user.Session.Pool.AcquireAllIdle(ctx) {
		if err := idleConnection.Conn().Close(ctx); err != nil {
			log.Dbg("failed to close idle connection after reset:", err)
		}

		idleConnection.Release()
	}

	cloneConn, err := user.Session.Pool.Acquire(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to acquire database connection after reset")
	}

	user.Session.ClonePoolConn = cloneConn
	user.Session.CloneConnection = cloneConn.Conn()

	return map[string]interface{}{"reset": true}, nil
}

// inRolledBackV2Tx runs fn inside a transaction on a dedicated pool
// connection and ALWAYS rolls it back.
func (s *ProcessingService) inRolledBackV2Tx(ctx context.Context, user *usermanager.User,
	txOptions pgx.TxOptions, fn func(tx pgx.Tx) error) error {
	serviceConn, err := user.Session.Pool.Acquire(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to acquire connection")
	}
	defer serviceConn.Release()

	tx, err := serviceConn.BeginTx(ctx, txOptions)
	if err != nil {
		return errors.Wrap(err, "failed to begin transaction")
	}

	defer func() {
		if rbErr := tx.Rollback(ctx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
			log.Dbg("failed to rollback transaction:", rbErr)
		}
	}()

	return fn(tx)
}

// queryV2TextLines runs a single-column query (EXPLAIN forms) and joins the
// returned lines with LF.
func queryV2TextLines(ctx context.Context, db querier.Querier, sql string) (string, error) {
	rows, err := db.Query(ctx, sql)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	lines := []string{}

	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return "", err
		}

		lines = append(lines, line)
	}

	if err := rows.Err(); err != nil {
		return "", err
	}

	return strings.Join(lines, "\n"), nil
}

// queryV2Snapshot runs a query and converts up to limit rows into JSON-ready
// objects.
func queryV2Snapshot(ctx context.Context, db querier.Querier, sql string, limit int) ([]interface{}, error) {
	rows, err := db.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	snapshot := []interface{}{}

	for rows.Next() {
		if len(snapshot) >= limit {
			break
		}

		fields := rows.FieldDescriptions()
		rawValues := rows.RawValues()
		row := make(map[string]interface{}, len(fields))

		for i, field := range fields {
			row[field.Name] = convertV2Value(field.DataTypeOID, rawValues[i])
		}

		snapshot = append(snapshot, row)
	}

	rows.Close()

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return snapshot, nil
}

// v2JSONNumberRe matches the JSON number grammar — the only server texts safe
// to carry as JSON numbers (rejects NaN/Infinity).
var v2JSONNumberRe = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)

// convertV2Value maps a text-format Postgres value to a JSON-ready value:
// SQL NULL -> nil, booleans -> bool, numeric-family values -> json.Number
// (arbitrary precision — the decimal contract forbids float64), everything
// else -> the server's text representation.
func convertV2Value(oid uint32, raw []byte) interface{} {
	if raw == nil {
		return nil
	}

	text := string(raw)

	switch oid {
	case pgtype.BoolOID:
		return text == "t"
	case pgtype.Int2OID, pgtype.Int4OID, pgtype.Int8OID, pgtype.OIDOID,
		pgtype.NumericOID, pgtype.Float4OID, pgtype.Float8OID:
		if v2JSONNumberRe.MatchString(text) {
			return json.Number(text)
		}

		return text
	default:
		return text
	}
}

// extractV2TerminatePID pulls the pid out of the dispatcher-composed
// terminate command string.
func extractV2TerminatePID(commandString string) string {
	match := v2TerminatePIDRe.FindStringSubmatch(commandString)
	if match == nil {
		return ""
	}

	return match[1]
}

func stringsToInterfaces(values []string) []interface{} {
	converted := make([]interface{}, len(values))
	for i, v := range values {
		converted[i] = v
	}

	return converted
}

// v2Notices captures Postgres notices raised on registered backend
// connections (initConn wires it into every clone connection).
var v2Notices = newNoticeRecorder()

type noticeRecorder struct {
	mu    sync.Mutex
	sinks map[*pgconn.PgConn][]string
}

func newNoticeRecorder() *noticeRecorder {
	return &noticeRecorder{sinks: make(map[*pgconn.PgConn][]string)}
}

// handle is the pgconn OnNotice hook. Capture is capped at v2NoticesCap
// notices per connection; further notices are dropped.
func (r *noticeRecorder) handle(conn *pgconn.PgConn, notice *pgconn.Notice) {
	r.mu.Lock()
	defer r.mu.Unlock()

	sink, ok := r.sinks[conn]
	if !ok || len(sink) >= v2NoticesCap {
		return
	}

	r.sinks[conn] = append(sink, formatV2Notice(notice))
}

// start begins capturing notices for a connection.
func (r *noticeRecorder) start(conn *pgconn.PgConn) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.sinks[conn] = []string{}
}

// stop ends capturing and returns the collected notices; nil when the
// connection was not captured.
func (r *noticeRecorder) stop(conn *pgconn.PgConn) []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	notices, ok := r.sinks[conn]
	if !ok {
		return nil
	}

	delete(r.sinks, conn)

	return notices
}

// formatV2Notice renders a notice the way psql/psycopg2 display it:
// "SEVERITY:  message".
func formatV2Notice(notice *pgconn.Notice) string {
	return fmt.Sprintf("%s:  %s", notice.Severity, notice.Message)
}

/*
2026 © Postgres.ai
*/

package msgproc

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"

	pgaiv2sdk "gitlab.com/postgres-ai/joe/pkg/pgai_v2_sdk"
	"gitlab.com/postgres-ai/joe/pkg/services/usermanager"
)

func TestConvertV2Value(t *testing.T) {
	cases := []struct {
		name     string
		oid      uint32
		raw      []byte
		expected interface{}
	}{
		{"sql null", pgtype.TextOID, nil, nil},
		{"bool true", pgtype.BoolOID, []byte("t"), true},
		{"bool false", pgtype.BoolOID, []byte("f"), false},
		{"int4", pgtype.Int4OID, []byte("42"), json.Number("42")},
		{"int8 beyond float64", pgtype.Int8OID, []byte("9007199254740993"), json.Number("9007199254740993")},
		{"numeric scale preserved", pgtype.NumericOID, []byte("1.50"), json.Number("1.50")},
		{"numeric NaN falls back to text", pgtype.NumericOID, []byte("NaN"), "NaN"},
		{"float with exponent", pgtype.Float8OID, []byte("1e+30"), json.Number("1e+30")},
		{"float infinity falls back to text", pgtype.Float8OID, []byte("Infinity"), "Infinity"},
		{"text", pgtype.TextOID, []byte("hello"), "hello"},
		{"timestamp stays text", pgtype.TimestamptzOID, []byte("2026-07-07 00:00:00+00"), "2026-07-07 00:00:00+00"},
		{"empty string", pgtype.TextOID, []byte(""), ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, convertV2Value(tc.oid, tc.raw))
		})
	}
}

func TestConvertV2ResultRows(t *testing.T) {
	fields := []pgconn.FieldDescription{
		{Name: "id", DataTypeOID: pgtype.Int4OID},
		{Name: "name", DataTypeOID: pgtype.TextOID},
	}
	rows := [][][]byte{
		{[]byte("1"), []byte("alice")},
		{[]byte("2"), []byte("bob")},
		{[]byte("3"), nil},
	}

	t.Run("converted with names and types", func(t *testing.T) {
		converted := convertV2ResultRows(fields, rows, 10)
		assert.Equal(t, []interface{}{
			map[string]interface{}{"id": json.Number("1"), "name": "alice"},
			map[string]interface{}{"id": json.Number("2"), "name": "bob"},
			map[string]interface{}{"id": json.Number("3"), "name": nil},
		}, converted)
	})

	t.Run("capped", func(t *testing.T) {
		assert.Len(t, convertV2ResultRows(fields, rows, 2), 2)
	})
}

func TestExtractV2TerminatePID(t *testing.T) {
	cases := []struct {
		commandString string
		expected      string
	}{
		{"select pg_catalog.pg_terminate_backend(66)", "66"},
		{"select pg_terminate_backend(123456)", "123456"},
		{"select 1", ""},
	}

	for _, tc := range cases {
		t.Run(tc.commandString, func(t *testing.T) {
			assert.Equal(t, tc.expected, extractV2TerminatePID(tc.commandString))
		})
	}
}

func TestV2CloneID(t *testing.T) {
	assert.Equal(t, "v2-31", v2CloneID("31"))
}

func TestNoticeRecorder(t *testing.T) {
	recorder := newNoticeRecorder()
	conn := &pgconn.PgConn{}
	otherConn := &pgconn.PgConn{}

	t.Run("uncaptured connections are ignored", func(t *testing.T) {
		recorder.handle(conn, &pgconn.Notice{Severity: "NOTICE", Message: "dropped"})
		assert.Nil(t, recorder.stop(conn))
	})

	t.Run("captures between start and stop, per connection", func(t *testing.T) {
		recorder.start(conn)
		recorder.handle(conn, &pgconn.Notice{Severity: "NOTICE", Message: "relation created"})
		recorder.handle(otherConn, &pgconn.Notice{Severity: "NOTICE", Message: "other conn"})
		recorder.handle(conn, &pgconn.Notice{Severity: "WARNING", Message: "watch out"})

		assert.Equal(t, []string{"NOTICE:  relation created", "WARNING:  watch out"}, recorder.stop(conn))
		assert.Nil(t, recorder.stop(conn), "stop clears the sink")
		assert.Nil(t, recorder.stop(otherConn), "uncaptured connection collected nothing")
	})

	t.Run("empty capture returns empty slice", func(t *testing.T) {
		recorder.start(conn)
		assert.Equal(t, []string{}, recorder.stop(conn))
	})

	t.Run("capture is capped at v2NoticesCap", func(t *testing.T) {
		recorder.start(conn)

		for i := 0; i < v2NoticesCap+10; i++ {
			recorder.handle(conn, &pgconn.Notice{Severity: "NOTICE", Message: strconv.Itoa(i)})
		}

		notices := recorder.stop(conn)
		assert.Len(t, notices, v2NoticesCap)
		assert.Equal(t, "NOTICE:  0", notices[0])
		assert.Equal(t, "NOTICE:  "+strconv.Itoa(v2NoticesCap-1), notices[v2NoticesCap-1])
	})
}

func TestV2Runner(t *testing.T) {
	s := &ProcessingService{}

	t.Run("every supported command routes to a runner", func(t *testing.T) {
		supported := []string{
			pgaiv2sdk.CommandPlan,
			pgaiv2sdk.CommandExplain,
			pgaiv2sdk.CommandExec,
			pgaiv2sdk.CommandHypo,
			pgaiv2sdk.CommandActivity,
			pgaiv2sdk.CommandDescribe,
			pgaiv2sdk.CommandTerminate,
			pgaiv2sdk.CommandReset,
		}

		for _, command := range supported {
			t.Run(command, func(t *testing.T) {
				assert.True(t, pgaiv2sdk.IsSupportedCommand(command),
					"the test list must mirror the dispatch validator")

				runner, err := s.v2Runner(command)
				assert.NoError(t, err)
				assert.NotNil(t, runner)
			})
		}
	})

	t.Run("commands are matched case-insensitively", func(t *testing.T) {
		runner, err := s.v2Runner("PLAN")
		assert.NoError(t, err)
		assert.NotNil(t, runner)
	})

	t.Run("unknown command errors", func(t *testing.T) {
		runner, err := s.v2Runner("drop")
		assert.Nil(t, runner)
		assert.EqualError(t, err, `unsupported v2 command "drop"`)
	})

	t.Run("hypo runner enforces args.query", func(t *testing.T) {
		runner, err := s.v2Runner(pgaiv2sdk.CommandHypo)
		assert.NoError(t, err)

		// runV2Hypo rejects a missing args.query before touching the session.
		result, err := runner(context.Background(), &usermanager.User{},
			&pgaiv2sdk.DispatchRequest{Command: pgaiv2sdk.CommandHypo})
		assert.Nil(t, result)
		assert.EqualError(t, err, "hypo dispatch carried no args.query")
	})
}

func TestExecuteV2CommandUnknownCommand(t *testing.T) {
	// A zero service suffices: an unsupported command must fail BEFORE any
	// session preparation (UserManager is nil here and must not be touched).
	s := &ProcessingService{}

	result, err := s.ExecuteV2Command(context.Background(), &pgaiv2sdk.DispatchRequest{
		SchemaVersion: pgaiv2sdk.SchemaVersion,
		CommandID:     "1",
		Command:       "vacuum",
		SessionID:     "31",
		Nonce:         "nonce",
		ReplyURL:      "http://reply.invalid",
	})

	assert.Nil(t, result)
	assert.EqualError(t, err, `unsupported v2 command "vacuum"`)
}

func TestLockV2Session(t *testing.T) {
	s := &ProcessingService{}

	t.Run("same session serializes", func(t *testing.T) {
		first := s.lockV2Session("31")

		acquired := make(chan struct{})

		go func() {
			second := s.lockV2Session("31")
			close(acquired)
			second()
		}()

		select {
		case <-acquired:
			t.Fatal("the second lock must block until the first unlock")
		case <-time.After(50 * time.Millisecond):
		}

		first()

		select {
		case <-acquired:
		case <-time.After(5 * time.Second):
			t.Fatal("the second lock was not acquired after the first unlock")
		}
	})

	t.Run("distinct sessions do not block each other", func(t *testing.T) {
		first := s.lockV2Session("31")
		defer first()

		acquired := make(chan struct{})

		go func() {
			other := s.lockV2Session("32")
			close(acquired)
			other()
		}()

		select {
		case <-acquired:
		case <-time.After(5 * time.Second):
			t.Fatal("a lock for a distinct session must not block")
		}
	})
}

func TestV2EnvelopeStripping(t *testing.T) {
	t.Run("plan envelope", func(t *testing.T) {
		commandString := v2PlanEnvelopePrefix + "select * from users where id = 1"
		sql := commandString[len(v2PlanEnvelopePrefix):]
		assert.Equal(t, "select * from users where id = 1", sql)
	})

	t.Run("explain envelope", func(t *testing.T) {
		commandString := v2ExplainEnvelopePrefix + "delete from users" + v2ExplainEnvelopeSuffix
		sql := commandString[len(v2ExplainEnvelopePrefix) : len(commandString)-len(v2ExplainEnvelopeSuffix)]
		assert.Equal(t, "delete from users", sql)
	})
}

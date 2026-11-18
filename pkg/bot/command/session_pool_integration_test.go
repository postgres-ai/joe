//go:build integration

/*
2026 © Postgres.ai
*/

package command

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"gitlab.com/postgres-ai/database-lab/v3/pkg/client/dblabapi"
	dblabmodels "gitlab.com/postgres-ai/database-lab/v3/pkg/models"

	"gitlab.com/postgres-ai/joe/pkg/models"
	"gitlab.com/postgres-ai/joe/pkg/services/platform"
	"gitlab.com/postgres-ai/joe/pkg/services/usermanager"
)

// These tests guard the session connection accounting against a real PostgreSQL
// instance: https://gitlab.com/postgres-ai/database-lab/-/work_items/818. Run them with:
//
//	JOE_TEST_PG_DSN='postgres://postgres@localhost:5432/postgres?sslmode=disable' \
//	    go test -tags integration ./pkg/bot/command/ -run SessionPool -v

const testCloneID = "joe-session-pool-test"

// testSession builds a session the way msgproc does, but with a pool of two
// connections: Explain needs both, so a single leaked slot is enough to starve it.
func testSession(t *testing.T, ctx context.Context) *usermanager.UserSession {
	t.Helper()

	dsn := os.Getenv("JOE_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("set JOE_TEST_PG_DSN to run session pool integration tests")
	}

	cfg, err := pgxpool.ParseConfig(dsn)
	require.NoError(t, err)

	cfg.MaxConns = 2
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)

	// Close blocks until every acquired connection is released, so a leaked slot would hang the test instead of failing it.
	t.Cleanup(func() { go pool.Close() })

	userConn, err := usermanager.ConnectUser(ctx, pool)
	require.NoError(t, err)

	session := &usermanager.UserSession{
		Clone: &dblabmodels.Clone{
			ID:       testCloneID,
			Snapshot: &dblabmodels.Snapshot{DataStateAt: time.Now().UTC().Format(time.RFC3339)},
		},
		ConnParams:      models.Clone{Name: cfg.ConnConfig.Database},
		Pool:            pool,
		CloneConnection: userConn,
	}

	t.Cleanup(func() { _ = session.CloneConnection.Close(context.Background()) })

	require.NoError(t, session.CloneConnection.QueryRow(ctx, "select current_setting('server_version_num')::int").Scan(&session.DBVersion))

	return session
}

// testDBLab serves the clone reset and status endpoints of the DBLab API.
func testDBLab(t *testing.T) *dblabapi.Client {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/reset") {
			w.WriteHeader(http.StatusOK)
			return
		}

		_ = json.NewEncoder(w).Encode(dblabmodels.Clone{
			ID:     testCloneID,
			Status: dblabmodels.Status{Code: dblabmodels.StatusOK},
		})
	}))
	t.Cleanup(srv.Close)

	client, err := dblabapi.NewClient(dblabapi.Options{Host: srv.URL})
	require.NoError(t, err)

	return client
}

// requireAcquired waits for the pool to report want acquired connections: pgxpool
// destroys a released closed connection in a goroutine, so the count settles shortly after Release.
func requireAcquired(t *testing.T, pool *pgxpool.Pool, want int32, msgAndArgs ...any) {
	t.Helper()

	require.Eventually(t, func() bool { return pool.Stat().AcquiredConns() == want },
		2*time.Second, 10*time.Millisecond, msgAndArgs...)
}

type testMessenger struct{}

func (testMessenger) Publish(*models.Message) error                              { return nil }
func (testMessenger) UpdateText(*models.Message) error                           { return nil }
func (testMessenger) UpdateStatus(*models.Message, models.MessageStatus) error   { return nil }
func (testMessenger) Fail(*models.Message, string) error                         { return nil }
func (testMessenger) OK(*models.Message) error                                   { return nil }
func (testMessenger) DownloadArtifact(string) ([]byte, error)                    { return nil, nil }
func (testMessenger) AddArtifact(string, string, string, string) (string, error) { return "", nil }

func TestSessionPoolResetsDoNotLeakConnections(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	session := testSession(t, ctx)
	dbLab := testDBLab(t)

	for i := 1; i <= 3; i++ {
		err := ResetSession(ctx, &platform.Command{}, models.NewMessage(models.IncomingMessage{}), dbLab,
			testMessenger{}, session, "test", "CE")
		require.NoError(t, err)
		requireAcquired(t, session.Pool, 0, "reset %d left a pool connection acquired", i)

		var one int
		require.NoError(t, session.CloneConnection.QueryRow(ctx, "select 1").Scan(&one),
			"reset %d left the user connection unusable", i)
	}

	cmd := &platform.Command{Query: "select 1"}
	require.NoError(t, Explain(ctx, testMessenger{}, cmd, models.NewMessage(models.IncomingMessage{}), *session))
	require.NotEmpty(t, cmd.PlanExecJSON)
	requireAcquired(t, session.Pool, 0, "explain left a pool connection acquired")
}

func TestSessionPoolExplainFailsWhenPoolIsBusy(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	session := testSession(t, ctx)

	defaultTimeout := connAcquireTimeout
	connAcquireTimeout = time.Second

	t.Cleanup(func() { connAcquireTimeout = defaultTimeout })

	busy, err := session.Pool.Acquire(ctx)
	require.NoError(t, err)

	defer busy.Release()

	// One free slot: Explain gets its service connection, then must fail on the
	// observer connection instead of waiting forever.
	err = Explain(ctx, testMessenger{}, &platform.Command{Query: "select 1"},
		models.NewMessage(models.IncomingMessage{}), *session)
	require.ErrorIs(t, err, errNoFreeConn)
	requireAcquired(t, session.Pool, 1, "explain left a pool connection acquired")
}

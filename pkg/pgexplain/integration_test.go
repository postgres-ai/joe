//go:build integration

/*
2026 © Postgres.ai
*/

// Package pgexplain integration smoke test.
//
// This file is guarded by the `integration` build tag, so it is invisible to
// the normal `go test ./pkg/...` run and only compiled/executed when the tag is
// passed explicitly (e.g. `go test -tags integration ./pkg/pgexplain/...`).
//
// It connects to a live PostgreSQL server (DSN taken from PGEXPLAIN_TEST_DSN),
// runs the EXACT EXPLAIN form that joe issues against a representative query
// set, and feeds the resulting JSON through NewExplain to make sure parsing and
// rendering never error or panic. Running this across PostgreSQL majors is what
// would have caught the PG18 fractional-rows break against a real server.
package pgexplain

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

// integrationExplainPrefix is the EXACT EXPLAIN form joe issues, built from the same
// pgexplain constants as command.analyzePrefix so the smoke test can't drift from
// production. The matrix runs PostgreSQL 13+, so the full SETTINGS + WAL form applies.
var integrationExplainPrefix = fmt.Sprintf(ExplainAnalyzeQuery, ExplainSettingsOption+ExplainWALOption)

// integrationQuery describes a single representative query plus the session
// setup it needs (e.g. disabling hash/merge joins to force a nested loop with
// inner loops > 1).
type integrationQuery struct {
	name  string
	setup []string // optional per-query session settings
	query string
}

func integrationQueries() []integrationQuery {
	return []integrationQuery{
		{
			name:  "seq_scan",
			query: "SELECT * FROM integration_big WHERE n > 100",
		},
		{
			name:  "index_scan",
			query: "SELECT * FROM integration_big WHERE id = 42",
		},
		{
			// Force a nested loop (inner loops > 1) by disabling the other join
			// strategies. This is the shape that exercises fractional-rows
			// handling on PG18.
			name: "nested_loop",
			setup: []string{
				"SET LOCAL enable_hashjoin = off",
				"SET LOCAL enable_mergejoin = off",
			},
			query: "SELECT b.id, s.label FROM integration_big b JOIN integration_small s ON s.id = (b.n % 5) + 1",
		},
		{
			name:  "aggregate_group",
			query: "SELECT n % 5 AS bucket, count(*), avg(n) FROM integration_big GROUP BY 1 ORDER BY 1",
		},
		{
			name:  "insert",
			query: "INSERT INTO integration_big (n) SELECT g FROM generate_series(1, 10) g",
		},
	}
}

func integrationConnect(t *testing.T) (*pgx.Conn, context.Context) {
	t.Helper()

	dsn := os.Getenv("PGEXPLAIN_TEST_DSN")
	if dsn == "" {
		t.Skip("PGEXPLAIN_TEST_DSN is not set; skipping live-DB integration smoke test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)

	// Pin a single connection (pgx.Connect returns exactly one *pgx.Conn).
	conn, err := pgx.Connect(ctx, dsn)
	require.NoError(t, err, "failed to connect with PGEXPLAIN_TEST_DSN")

	t.Cleanup(func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer closeCancel()
		_ = conn.Close(closeCtx)
	})

	return conn, ctx
}

func integrationSetupSchema(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()

	stmts := []string{
		"DROP TABLE IF EXISTS integration_big",
		"DROP TABLE IF EXISTS integration_small",
		"CREATE TABLE integration_big (id serial PRIMARY KEY, n int NOT NULL)",
		"CREATE TABLE integration_small (id int PRIMARY KEY, label text NOT NULL)",
		// A few hundred rows in the big table.
		"INSERT INTO integration_big (n) SELECT g FROM generate_series(1, 500) g",
		// A 5-row small table to drive the nested-loop join.
		"INSERT INTO integration_small (id, label) SELECT g, 'label-' || g FROM generate_series(1, 5) g",
		"ANALYZE integration_big",
		"ANALYZE integration_small",
	}

	for _, stmt := range stmts {
		_, err := conn.Exec(ctx, stmt)
		require.NoErrorf(t, err, "schema setup failed for statement: %s", stmt)
	}
}

func integrationDropSchema(ctx context.Context, conn *pgx.Conn) {
	_, _ = conn.Exec(ctx, "DROP TABLE IF EXISTS integration_big")
	_, _ = conn.Exec(ctx, "DROP TABLE IF EXISTS integration_small")
}

// integrationExplainJSON runs the EXACT EXPLAIN form joe uses and returns the
// single-row JSON text. Per-query session settings are applied inside a
// transaction so SET LOCAL takes effect and is rolled back afterwards.
func integrationExplainJSON(t *testing.T, ctx context.Context, conn *pgx.Conn, q integrationQuery) string {
	t.Helper()

	tx, err := conn.Begin(ctx)
	require.NoError(t, err, "failed to begin transaction")
	defer func() { _ = tx.Rollback(ctx) }()

	for _, s := range q.setup {
		_, err := tx.Exec(ctx, s)
		require.NoErrorf(t, err, "failed to apply session setting: %s", s)
	}

	var planJSON string
	err = tx.QueryRow(ctx, integrationExplainPrefix+q.query).Scan(&planJSON)
	require.NoError(t, err, "failed to read EXPLAIN JSON")

	return planJSON
}

func TestIntegrationExplainAgainstLiveServer(t *testing.T) {
	conn, ctx := integrationConnect(t)

	integrationSetupSchema(t, ctx, conn)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		integrationDropSchema(cleanupCtx, conn)
	})

	for _, q := range integrationQueries() {
		t.Run(q.name, func(t *testing.T) {
			planJSON := integrationExplainJSON(t, ctx, conn, q)
			require.NotEmpty(t, planJSON, "EXPLAIN returned empty JSON")

			explain, err := NewExplain(planJSON)
			require.NoError(t, err, "NewExplain failed to parse the EXPLAIN JSON")
			require.NotNil(t, explain)

			require.NotEmpty(t, explain.RenderPlanText(), "RenderPlanText returned empty output")

			require.NotPanics(t, func() {
				_ = explain.RenderPlanText()
				_ = explain.RenderStats()
			}, "rendering must not panic")
		})
	}
}

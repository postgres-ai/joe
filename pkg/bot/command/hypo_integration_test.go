//go:build integration

/*
2026 © Postgres.ai
*/

package command

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// These tests exercise joe's HypoPG queries against a real PostgreSQL instance
// with the HypoPG extension installed. They guard against upstream HypoPG API
// changes (e.g. HypoPG 1.2.0 turned hypopg_list_indexes() from a function into a
// view and renamed its columns, which broke `hypo desc`). Run them with:
//
//	JOE_TEST_HYPOPG_DSN='postgres://postgres:postgres@localhost:55432/joe_test' \
//	    go test -tags integration ./pkg/bot/command/ -run Hypo -v
//
// The DSN must point at a database where the HypoPG extension is available.

// hypoTestConn dials the test database and pins a single backend connection:
// hypothetical indexes created via HypoPG are visible only within the backend
// that created them, so create/list/describe must share one connection.
func hypoTestConn(t *testing.T) (context.Context, *pgxpool.Conn) {
	t.Helper()

	dsn := os.Getenv("JOE_TEST_HYPOPG_DSN")
	if dsn == "" {
		t.Skip("set JOE_TEST_HYPOPG_DSN to run HypoPG integration tests")
	}

	ctx := context.Background()

	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	conn, err := pool.Acquire(ctx)
	require.NoError(t, err)
	t.Cleanup(conn.Release)

	_, err = conn.Exec(ctx, "create extension if not exists hypopg")
	require.NoError(t, err)

	return ctx, conn
}

// seedHypoIndex creates a throwaway table and one hypothetical index on it,
// returning the index' indexrelid (as text) and name. State is reset on cleanup.
func seedHypoIndex(t *testing.T, ctx context.Context, conn *pgxpool.Conn) (indexrelid, indexname string) {
	t.Helper()

	_, err := conn.Exec(ctx, "create table if not exists joe_hypo_test(id int, val text)")
	require.NoError(t, err)

	_, err = conn.Exec(ctx, "select hypopg_reset()")
	require.NoError(t, err)

	err = conn.QueryRow(ctx,
		"select indexrelid::text, indexname from hypopg_create_index($1)",
		"create index on joe_hypo_test (id)",
	).Scan(&indexrelid, &indexname)
	require.NoError(t, err)
	require.NotEmpty(t, indexrelid)
	require.NotEmpty(t, indexname)

	t.Cleanup(func() {
		_, _ = conn.Exec(ctx, "select hypopg_reset()")
		_, _ = conn.Exec(ctx, "drop table if exists joe_hypo_test")
	})

	return indexrelid, indexname
}

// TestListHypoIndexes_Integration covers explain.go's listHypoIndexes, used to
// flag plans that involve a hypothetical index.
func TestListHypoIndexes_Integration(t *testing.T) {
	ctx, conn := hypoTestConn(t)
	_, indexname := seedHypoIndex(t, ctx, conn)

	names, err := listHypoIndexes(ctx, conn)
	require.NoError(t, err) // before the fix: function hypopg_list_indexes() does not exist (42883)
	require.Contains(t, names, indexname)
}

// TestDescribeHypoIndexes_ListAll_Integration covers `hypo desc` (no argument).
func TestDescribeHypoIndexes_ListAll_Integration(t *testing.T) {
	ctx, conn := hypoTestConn(t)
	_, indexname := seedHypoIndex(t, ctx, conn)

	res, err := describeHypoIndexes(ctx, conn, "")
	require.NoError(t, err) // before the fix: 42883
	require.GreaterOrEqual(t, len(res), 2, "want a header row plus at least one index row")
	require.Contains(t, flattenRows(res), indexname)
}

// TestDescribeHypoIndexes_One_Integration covers `hypo desc <indexrelid>`.
func TestDescribeHypoIndexes_One_Integration(t *testing.T) {
	ctx, conn := hypoTestConn(t)
	indexrelid, indexname := seedHypoIndex(t, ctx, conn)

	res, err := describeHypoIndexes(ctx, conn, indexrelid)
	require.NoError(t, err) // before the fix: 42883
	require.Len(t, res, 2, "want a header row plus exactly the one described index")

	row := strings.Join(res[1], " ")
	require.Contains(t, row, indexname)
	require.Contains(t, row, "joe_hypo_test", "hypopg_get_indexdef should mention the indexed table")
}

func flattenRows(res [][]string) string {
	var b strings.Builder

	for _, row := range res {
		for _, cell := range row {
			b.WriteString(cell)
			b.WriteByte(' ')
		}
	}

	return b.String()
}

//go:build integration

/*
2026 © Postgres.ai
*/

// Byte-fidelity guard for pkg/pgexplain's JSON->text renderer.
//
// pkg/pgexplain's contract is to byte-reproduce PostgreSQL's native text EXPLAIN
// (FORMAT TEXT). This guard enforces that contract against a *live* server: for
// every query in the fixture battery it captures the SAME plan twice — once as
// FORMAT JSON (rendered through joe's NewExplain(...).RenderPlanText()) and once
// as FORMAT TEXT (psql's own renderer) — then asserts the two plan bodies are
// byte-identical after normalize() removes the handful of documented, intentional
// differences between joe and psql.
//
// It is guarded by the `integration` build tag and reuses the same live-DB
// harness as integration_test.go (PGEXPLAIN_TEST_DSN, integrationConnect). Run it
// across PostgreSQL majors:
//
//	PGEXPLAIN_TEST_DSN=postgres://... go test -tags integration ./pkg/pgexplain/... \
//	    -run TestFidelityAgainstLiveServer -v
//
// Unlike the Integration smoke test (which only checks NewExplain never panics),
// this guard fails on ANY new plan-body divergence, so it catches the JSON->text
// rendering gaps (missing lines, wrong captions) that joe-rendered goldens cannot.
package pgexplain

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

// fidelityExplainJSON / fidelityExplainText are the EXACT option string joe issues
// (pkg/pgexplain.ExplainAnalyzeQuery with no version-gated SETTINGS/WAL, matching
// the fidelity spec), differing only in the output FORMAT. Deriving both from the
// production constant keeps the guard from drifting from what joe really runs.
var (
	fidelityExplainJSON = fmt.Sprintf(ExplainAnalyzeQuery, "")
	fidelityExplainText = strings.Replace(fidelityExplainJSON, "FORMAT JSON", "FORMAT TEXT", 1)
)

// --- normalize(): reconcile joe's output with psql FORMAT TEXT --------------
//
// normalize() strips EXACTLY the differences pkg/pgexplain deliberately keeps
// from psql's text format, so that what remains is the plan body both must agree
// on byte-for-byte. Four are documented, intentional renderer traits; the rest is
// a deliberately small allowlist of known, separately-tracked rendering gaps, each
// commented with why it is safe to ignore here.

// maskCost / maskActual blank out the per-node cost and actual-timing clauses
// (trait 4: cost/rows/width are planner output, actual time/rows/loops are per-run
// and differ between the two captures). The two clauses are replaced wholesale so
// no volatile number survives, while their presence/position still anchors the line.
var (
	maskCost   = regexp.MustCompile(`\(cost=[0-9]+\.[0-9]+\.\.[0-9]+\.[0-9]+ rows=[0-9]+ width=[0-9]+\)`)
	maskActual = regexp.MustCompile(`\(actual time=[0-9]+\.[0-9]+\.\.[0-9]+\.[0-9]+ rows=[0-9]+(\.[0-9]+)? loops=[0-9]+\)`)
	// maskKB masks memory/disk sizes (trait 4): "Memory: 25kB", "Disk: 960kB",
	// "Memory Usage: 9kB", "Maximum Storage: NkB" all carry run-variable kB values.
	maskKB = regexp.MustCompile(`[0-9]+kB`)
	// maskWorkersLaunched masks the launched-worker count (trait 4): how many
	// parallel workers actually start is run-variable (0..planned).
	maskWorkersLaunched = regexp.MustCompile(`Workers Launched: [0-9]+`)
	// workerLine matches a per-worker detail header ("Worker 0:  actual time=...").
	workerLine = regexp.MustCompile(`^Worker [0-9]+:`)
)

// fidelityTrailingMarkers are the trimmed prefixes of the trailing stats block
// (trait 3). joe renders these separately (RenderStats) or not at all, and psql
// appends them after the plan body, so the body comparison stops at the first one.
var fidelityTrailingMarkers = []string{
	"Planning Time:",
	"Planning:", // per-plan "Planning:\n  Buffers:" block
	"Execution Time:",
	"Settings:",
	"JIT:",
	"Query Identifier:",
	"Query ID:",
}

// leadingSpaces returns the number of leading space characters, used to detect a
// per-worker line's subordinate (more-indented) detail lines.
func leadingSpaces(s string) int {
	return len(s) - len(strings.TrimLeft(s, " "))
}

// isTrailingMarker reports whether a leading-trimmed line begins the trailing
// stats block.
func isTrailingMarker(trimmed string) bool {
	for _, m := range fidelityTrailingMarkers {
		if strings.HasPrefix(trimmed, m) {
			return true
		}
	}

	return false
}

// normalize turns a rendered plan (joe's or psql's) into the canonical plan body
// used for the byte comparison. It is intentionally applied to BOTH sides so any
// residual difference is a real fidelity bug, not a normalization artifact.
func normalize(s string) string {
	rawLines := strings.Split(s, "\n")

	// Pass 1 (indentation-aware, before indentation is stripped): cut the trailing
	// stats block, and drop each per-worker block — the "Worker N:" line plus its
	// deeper-indented children (joe does not render per-worker detail; #210).
	var body []string
	for i := 0; i < len(rawLines); i++ {
		line := rawLines[i]
		trimmed := strings.TrimLeft(line, " ")

		if isTrailingMarker(trimmed) {
			break // trait 3: everything from here on is the trailing stats block
		}

		if workerLine.MatchString(trimmed) {
			// ALLOWLIST (#210 per-worker output): drop this worker header and every
			// subsequent line indented deeper than it (its per-worker Buffers/Sort).
			indent := leadingSpaces(line)
			for i+1 < len(rawLines) && strings.TrimSpace(rawLines[i+1]) != "" &&
				leadingSpaces(rawLines[i+1]) > indent {
				i++
			}

			continue
		}

		body = append(body, line)
	}

	// Pass 2 (per line): strip indentation + tree prefixes, drop suppressed lines,
	// and mask the remaining volatile tokens.
	var out []string
	for _, line := range body {
		l := strings.TrimLeft(line, " ")  // trait 1: indentation
		l = strings.TrimPrefix(l, "->  ") // trait 1: tree prefix
		l = strings.TrimLeft(l, " ")

		if l == "" {
			continue
		}
		if strings.HasPrefix(l, "Output:") {
			continue // trait 2: joe suppresses VERBOSE per-node Output: lines
		}
		if strings.HasPrefix(l, "Replaces:") {
			// ALLOWLIST (pg19): PostgreSQL 19 adds a "Replaces: Scan on <rel>" line
			// on some Result nodes that joe does not render yet.
			continue
		}
		if strings.HasPrefix(l, "Inner Unique:") {
			// ALLOWLIST (#210): joe does not render the join "Inner Unique:" line.
			continue
		}

		// trait 4: mask the run-variable numeric tokens.
		l = maskCost.ReplaceAllString(l, "(cost)")
		l = maskActual.ReplaceAllString(l, "(actual)")
		if strings.HasPrefix(l, "Buffers:") {
			l = "Buffers:" // trait 4: hit/read counts differ per capture
		}
		if strings.HasPrefix(l, "I/O Timings:") {
			l = "I/O Timings:" // trait 4: per-run I/O timings
		}
		l = maskKB.ReplaceAllString(l, "NkB")
		l = maskWorkersLaunched.ReplaceAllString(l, "Workers Launched: N")

		out = append(out, l)
	}

	return strings.Join(out, "\n")
}

// --- fixture battery --------------------------------------------------------

// fidelityCase is one battery entry: a name, optional per-query SET LOCAL setup,
// and the query whose plan is compared.
type fidelityCase struct {
	name  string
	setup []string
	query string
}

// parseFidelityQueries parses testdata/fidelity/queries.sql (see that file's
// header for the record format).
func parseFidelityQueries(t *testing.T, path string) []fidelityCase {
	t.Helper()

	raw, err := os.ReadFile(path)
	require.NoError(t, err, "read fixture battery %s", path)

	var (
		cases []fidelityCase
		cur   fidelityCase
	)
	flush := func() {
		if cur.query != "" {
			cases = append(cases, cur)
		}
		cur = fidelityCase{}
	}

	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "":
			flush()
		case strings.HasPrefix(trimmed, "-- name:"):
			cur.name = strings.TrimSpace(strings.TrimPrefix(trimmed, "-- name:"))
		case strings.HasPrefix(trimmed, "-- setup:"):
			for _, s := range strings.Split(strings.TrimPrefix(trimmed, "-- setup:"), ";") {
				if s = strings.TrimSpace(s); s != "" {
					cur.setup = append(cur.setup, s)
				}
			}
		case strings.HasPrefix(trimmed, "--"):
			// human-readable note; ignore
		default:
			cur.query = strings.TrimSuffix(trimmed, ";")
		}
	}
	flush()

	require.NotEmpty(t, cases, "no fixtures parsed from %s", path)

	return cases
}

// execScript strips `--` comment lines from a .sql file and runs each
// `;`-terminated statement, so the multi-statement seed can be applied with pgx's
// per-statement Exec.
func execScript(t *testing.T, ctx context.Context, conn *pgx.Conn, path string) {
	t.Helper()

	raw, err := os.ReadFile(path)
	require.NoError(t, err, "read seed script %s", path)

	var sb strings.Builder
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		sb.WriteString(line)
		sb.WriteString("\n")
	}

	for _, stmt := range strings.Split(sb.String(), ";") {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		_, err := conn.Exec(ctx, stmt)
		require.NoErrorf(t, err, "seed statement failed: %s", strings.TrimSpace(stmt))
	}
}

// fidelityCapture runs both EXPLAIN forms for one case inside a single
// transaction (so SET LOCAL applies to both and is rolled back afterwards) and
// returns joe's rendered plan and psql's FORMAT TEXT plan.
func fidelityCapture(t *testing.T, ctx context.Context, conn *pgx.Conn, c fidelityCase) (joeBody, psqlText string) {
	t.Helper()

	tx, err := conn.Begin(ctx)
	require.NoError(t, err, "begin tx")
	defer func() { _ = tx.Rollback(ctx) }()

	for _, s := range c.setup {
		_, err := tx.Exec(ctx, s)
		require.NoErrorf(t, err, "setup failed: %s", s)
	}

	// FORMAT TEXT returns one row per plan line; join them back into the plan text.
	rows, err := tx.Query(ctx, fidelityExplainText+c.query)
	require.NoError(t, err, "EXPLAIN FORMAT TEXT")

	var textLines []string
	for rows.Next() {
		var line string
		require.NoError(t, rows.Scan(&line))
		textLines = append(textLines, line)
	}
	require.NoError(t, rows.Err())
	psqlText = strings.Join(textLines, "\n")

	// FORMAT JSON is a single-row document; render it through joe.
	var planJSON string
	require.NoError(t, tx.QueryRow(ctx, fidelityExplainJSON+c.query).Scan(&planJSON), "EXPLAIN FORMAT JSON")

	explain, err := NewExplain(planJSON)
	require.NoError(t, err, "NewExplain failed to parse EXPLAIN JSON")
	joeBody = explain.RenderPlanText()

	return joeBody, psqlText
}

func TestFidelityAgainstLiveServer(t *testing.T) {
	conn, ctx := integrationConnect(t)

	serverVersionNum := integrationServerVersionNum(t, ctx, conn)
	t.Logf("fidelity guard against server_version_num=%d", serverVersionNum)

	execScript(t, ctx, conn, "testdata/fidelity/schema.sql")
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = conn.Exec(cleanupCtx, "DROP TABLE IF EXISTS items, cats, ta, tb, tidtest, big CASCADE")
	})

	for _, c := range parseFidelityQueries(t, "testdata/fidelity/queries.sql") {
		c := c
		t.Run(c.name, func(t *testing.T) {
			joeBody, psqlText := fidelityCapture(t, ctx, conn, c)

			wantBody := normalize(psqlText)
			gotBody := normalize(joeBody)

			require.Equal(t, wantBody, gotBody,
				"pkg/pgexplain plan body diverges from psql FORMAT TEXT for %q.\n"+
					"want = normalized psql FORMAT TEXT, got = normalized joe RenderPlanText.\n"+
					"--- psql FORMAT TEXT (raw) ---\n%s\n--- joe RenderPlanText (raw) ---\n%s",
				c.name, psqlText, joeBody)
		})
	}
}

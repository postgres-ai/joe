/*
2026 © Postgres.ai
*/

package pgexplain

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// renderFixture parses a committed EXPLAIN JSON fixture and returns the rendered
// plan text. The fixtures under testdata/pg17 are REAL plans captured with the
// exact EXPLAIN form joe issues (FORMAT JSON, ANALYZE, VERBOSE, BUFFERS, SETTINGS,
// WAL); the assertions below compare joe's output against PostgreSQL's own
// FORMAT TEXT wording for the same plan.
func renderFixture(t *testing.T, path string) string {
	t.Helper()

	raw, err := os.ReadFile(path)
	require.NoError(t, err, "read fixture %s", path)

	// NewExplain succeeding is itself part of the contract: a wrong Go type for
	// any added field (e.g. []string for a scalar "Run Condition") makes the
	// non-strict decoder fail the WHOLE plan, not just that field.
	explain, err := NewExplain(string(raw))
	require.NoError(t, err, "NewExplain failed for %s", path)

	return explain.RenderPlanText()
}

// TestDroppedFieldMemoize (B1) checks that joe renders the Memoize cache stats that
// PostgreSQL prints for a Memoize node: Cache Key, Cache Mode and the hit/miss line.
func TestDroppedFieldMemoize(t *testing.T) {
	out := renderFixture(t, "testdata/pg17/memoize.json")

	require.Contains(t, out, "Cache Key: i.cat")
	require.Contains(t, out, "Cache Mode: logical")
	require.Contains(t, out, "Hits: 99950  Misses: 50  Evictions: 0  Overflows: 0  Memory Usage: 4kB")
}

// TestDroppedFieldJoinFilter (B2) checks the join-node Join Filter and its removed
// row count, which PostgreSQL prints on a Nested Loop with a non-pushed-down qual.
func TestDroppedFieldJoinFilter(t *testing.T) {
	out := renderFixture(t, "testdata/pg17/join_filter.json")

	require.Contains(t, out, "Join Filter: (c1.id <> c2.id)")
	require.Contains(t, out, "Rows Removed by Join Filter: 50")
}

// TestDroppedFieldJoinFilterZeroRemoved (B2) guards the >0 suppression: PostgreSQL
// omits the "Rows Removed by Join Filter" line when nothing was removed, so joe
// must render the Join Filter but not a "Rows Removed by Join Filter: 0" line.
func TestDroppedFieldJoinFilterZeroRemoved(t *testing.T) {
	const j = `[{
		"Plan": {
			"Node Type": "Nested Loop", "Parallel Aware": false, "Join Type": "Inner",
			"Startup Cost": 0.0, "Total Cost": 1.0, "Plan Rows": 1, "Plan Width": 0,
			"Actual Startup Time": 0.0, "Actual Total Time": 0.1, "Actual Rows": 1.00, "Actual Loops": 1,
			"Join Filter": "(a.x < b.y)", "Rows Removed by Join Filter": 0
		},
		"Planning Time": 0.1, "Triggers": [], "Execution Time": 0.1
	}]`

	explain, err := NewExplain(j)
	require.NoError(t, err)

	out := explain.RenderPlanText()
	require.Contains(t, out, "Join Filter: (a.x < b.y)")
	require.NotContains(t, out, "Rows Removed by Join Filter",
		"zero removed-row count must be suppressed, matching PostgreSQL")
}

// TestDroppedFieldPresortedKey (B3) checks the Incremental Sort "Presorted Key"
// line, which PostgreSQL prints right after "Sort Key".
func TestDroppedFieldPresortedKey(t *testing.T) {
	out := renderFixture(t, "testdata/pg17/incremental_sort.json")

	require.Contains(t, out, "Sort Key: isort_t.a, isort_t.b")
	require.Contains(t, out, "Presorted Key: isort_t.a")
}

// TestDroppedFieldSortGroups (B4) checks the Incremental Sort full-sort and
// pre-sorted group statistics emitted under EXPLAIN ANALYZE.
func TestDroppedFieldSortGroups(t *testing.T) {
	out := renderFixture(t, "testdata/pg17/incremental_sort.json")

	require.Contains(t, out, "Full-sort Groups: 1  Sort Method: quicksort  Average Memory: 27kB  Peak Memory: 27kB")
	require.Contains(t, out, "Pre-sorted Groups: 1  Sort Method: top-N heapsort  Average Memory: 28kB  Peak Memory: 28kB")
}

// TestDroppedFieldSortGroupsDiskSpill (B4) covers the on-disk branch that no
// captured fixture exercises: a group that spilled reports "Average Disk/Peak
// Disk" instead of the in-memory "Average Memory/Peak Memory" suffix.
func TestDroppedFieldSortGroupsDiskSpill(t *testing.T) {
	const j = `[{
		"Plan": {
			"Node Type": "Incremental Sort", "Parallel Aware": false,
			"Startup Cost": 0.0, "Total Cost": 1.0, "Plan Rows": 1, "Plan Width": 0,
			"Actual Startup Time": 0.0, "Actual Total Time": 0.1, "Actual Rows": 1.00, "Actual Loops": 1,
			"Sort Key": ["t.a", "t.b"], "Presorted Key": ["t.a"],
			"Pre-sorted Groups": {
				"Group Count": 3, "Sort Methods Used": ["external merge"],
				"Sort Space Disk": {"Average Sort Space Used": 2048, "Peak Sort Space Used": 2048}
			}
		},
		"Planning Time": 0.1, "Triggers": [], "Execution Time": 0.1
	}]`

	explain, err := NewExplain(j)
	require.NoError(t, err)

	out := explain.RenderPlanText()
	require.Contains(t, out,
		"Pre-sorted Groups: 3  Sort Method: external merge  Average Disk: 2048kB  Peak Disk: 2048kB")
	require.NotContains(t, out, "Average Memory",
		"a disk-spilled group must not render the in-memory suffix")
}

// TestDroppedFieldBitmapRecheck (B5/B6) checks the Bitmap Heap Scan "Recheck Cond",
// the lossy-page "Rows Removed by Index Recheck" count, and the "Heap Blocks" line.
func TestDroppedFieldBitmapRecheck(t *testing.T) {
	out := renderFixture(t, "testdata/pg17/bitmap_heap_recheck.json")

	require.Contains(t, out, "Recheck Cond: ((recheck_t.v >= 1) AND (recheck_t.v <= 200000))")
	require.Contains(t, out, "Rows Removed by Index Recheck: 1982")
	require.Contains(t, out, "Heap Blocks: exact=582 lossy=8268")
}

// TestDroppedFieldHashAggSpill (B7) checks that a hashed aggregate that spilled to
// disk renders PostgreSQL's memory/spill line verbatim.
func TestDroppedFieldHashAggSpill(t *testing.T) {
	out := renderFixture(t, "testdata/pg17/hashagg_spill.json")

	require.Contains(t, out, "Planned Partitions: 4  Batches: 85  Memory Usage: 137kB  Disk Usage: 1880kB")
}

// TestDroppedFieldHashAggHavingOrder (B7) pins PostgreSQL's line ordering for a
// hashed aggregate with a HAVING qual: Group Key, then Filter, then the HashAgg
// memory line, then Rows Removed by Filter. joe used to emit the memory line
// before the Filter block, which mis-ordered a HashAggregate with HAVING.
func TestDroppedFieldHashAggHavingOrder(t *testing.T) {
	out := renderFixture(t, "testdata/pg17/hashagg_having.json")

	require.Contains(t, out, "Filter: (count(*) > 100)")
	require.Contains(t, out, "Batches: 1  Memory Usage: 53kB")
	require.Contains(t, out, "Rows Removed by Filter: 100")

	iFilter := strings.Index(out, "Filter: (count(*) > 100)")
	iInfo := strings.Index(out, "Batches: 1  Memory Usage: 53kB")
	iRemoved := strings.Index(out, "Rows Removed by Filter: 100")
	require.Less(t, iFilter, iInfo, "Filter must precede the HashAgg memory line")
	require.Less(t, iInfo, iRemoved, "HashAgg memory line must precede Rows Removed by Filter")
}

// TestDroppedFieldRunCondition (B8) checks the WindowAgg "Run Condition". The
// fixture carries a multi-condition Run Condition, which PostgreSQL emits as a
// single scalar string (not an array); decoding it must not fail the plan.
func TestDroppedFieldRunCondition(t *testing.T) {
	out := renderFixture(t, "testdata/pg17/window_run_condition.json")

	require.Contains(t, out, "Run Condition: ((row_number() OVER (?) <= 10) AND (rank() OVER (?) <= 20))")
}

// TestDroppedFieldRunConditionDecodeSafety (B8) is a focused decode-safety guard:
// a WindowAgg JSON whose "Run Condition" is a multi-clause scalar string must
// decode without error. If "Run Condition" were typed as []string, the non-strict
// decoder would reject the whole plan.
func TestDroppedFieldRunConditionDecodeSafety(t *testing.T) {
	const j = `[{
		"Plan": {
			"Node Type": "WindowAgg", "Parallel Aware": false,
			"Startup Cost": 0.35, "Total Cost": 3094.39, "Plan Rows": 50157, "Plan Width": 38,
			"Actual Startup Time": 0.011, "Actual Total Time": 0.032, "Actual Rows": 10.00, "Actual Loops": 1,
			"Run Condition": "((row_number() OVER (?) <= 10) AND (rank() OVER (?) <= 20))"
		},
		"Planning Time": 0.1, "Triggers": [], "Execution Time": 0.1
	}]`

	explain, err := NewExplain(j)
	require.NoError(t, err, "scalar multi-clause Run Condition must not fail decoding")
	require.Contains(t, explain.RenderPlanText(),
		"Run Condition: ((row_number() OVER (?) <= 10) AND (rank() OVER (?) <= 20))")
}

// TestDroppedFieldsDecodeSafety guards against a wrong Go type for any added field:
// every new fixture must decode cleanly (scalar-vs-array mismatches turn into a
// whole-plan parse failure under the non-strict decoder).
func TestDroppedFieldsDecodeSafety(t *testing.T) {
	fixtures := []string{
		"testdata/pg17/memoize.json",
		"testdata/pg17/join_filter.json",
		"testdata/pg17/incremental_sort.json",
		"testdata/pg17/bitmap_heap_recheck.json",
		"testdata/pg17/hashagg_spill.json",
		"testdata/pg17/hashagg_having.json",
		"testdata/pg17/window_run_condition.json",
	}

	for _, f := range fixtures {
		t.Run(f, func(t *testing.T) {
			raw, err := os.ReadFile(f)
			require.NoError(t, err)

			_, err = NewExplain(string(raw))
			require.NoError(t, err, "fixture %s must decode without error", f)
		})
	}
}

// TestDroppedFieldsNoRegression proves the new render branches stay dormant for a
// plan that has none of the affected node types: the Seq Scan fixture must (1)
// render byte-identical to its committed golden, and (2) contain none of the new
// field labels. (2) is what makes this distinct from TestGolden's byte-identity
// check — it pins that the added code paths produce nothing for an unrelated plan.
func TestDroppedFieldsNoRegression(t *testing.T) {
	const fixture = "testdata/pg17/seq_scan.json"

	got := goldenRender(t, fixture)

	want, err := os.ReadFile(goldenPathFor(fixture))
	require.NoError(t, err)
	require.Equal(t, string(want), got,
		"a fixture without any dropped-field node type must render byte-identical to its golden")

	for _, label := range []string{
		"Cache Key:", "Cache Mode:", "Hits: ", "Join Filter:", "Presorted Key:",
		"Full-sort Groups:", "Pre-sorted Groups:", "Recheck Cond:",
		"Rows Removed by Index Recheck:", "Heap Blocks:", "Planned Partitions:",
		"Disk Usage:", "Run Condition:",
	} {
		require.NotContains(t, got, label,
			"a Seq Scan plan must not trigger the %q dropped-field branch", label)
	}
}

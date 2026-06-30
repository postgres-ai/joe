/*
2026 © Postgres.ai
*/

package pgexplain

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// renderCorrectnessFixture parses a real EXPLAIN (FORMAT JSON) plan captured from
// a live PostgreSQL server (testdata/correctness/<name>.json) and returns joe's
// rendered plan text. The Test* functions that call it then compare that text
// against the wording PostgreSQL itself emits for the same plan with
// EXPLAIN (FORMAT TEXT).
func renderCorrectnessFixture(t *testing.T, name string) string {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("testdata", "correctness", name+".json"))
	require.NoError(t, err, "read correctness fixture %s", name)

	explain, err := NewExplain(string(raw))
	require.NoError(t, err, "NewExplain failed for %s", name)

	return explain.RenderPlanText()
}

// TestRenderAggregateStrategyLabels covers A1. PostgreSQL names the Aggregate node
// after its strategy: Plain->"Aggregate", Sorted->"GroupAggregate",
// Hashed->"HashAggregate", Mixed->"MixedAggregate". joe previously special-cased
// only Hashed and rendered every other strategy as the bare "Aggregate".
//
// PG-native: " GroupAggregate  (cost=0.29..1199.93 ...)"
// joe (old): " Aggregate  (cost=0.29..1199.93 ...)"
func TestRenderAggregateStrategyLabels(t *testing.T) {
	out := renderCorrectnessFixture(t, "aggregate_groupagg")

	require.Contains(t, out, "GroupAggregate  (cost=",
		"a Sorted-strategy Aggregate must render as GroupAggregate, matching PostgreSQL")
	require.NotContains(t, out, " Aggregate  (cost=",
		"a Sorted-strategy Aggregate must not render as the bare Aggregate")
}

// TestRenderAggregatePartialMode covers A1's Partial Mode prefix. A parallel grouped
// aggregate splits into a leader-side "Finalize GroupAggregate" (Sorted) over a
// worker-side "Partial HashAggregate" (Hashed). joe previously dropped the
// Finalize/Partial word and mislabeled the Sorted node as "Aggregate".
//
// PG-native: " Finalize GroupAggregate  (..." and "Partial HashAggregate  (..."
// joe (old): " Aggregate  (..." and "HashAggregate  (..."
func TestRenderAggregatePartialMode(t *testing.T) {
	out := renderCorrectnessFixture(t, "aggregate_parallel_partial")

	require.Contains(t, out, "Finalize GroupAggregate  (cost=",
		"a Finalize/Sorted Aggregate must render as Finalize GroupAggregate")
	require.Contains(t, out, "Partial HashAggregate  (cost=",
		"a Partial/Hashed Aggregate must render as Partial HashAggregate")
	require.NotContains(t, out, " Aggregate  (cost=",
		"no aggregate node should render as the bare Aggregate in this plan")
}

// TestRenderNeverExecuted covers A2. A node whose Actual Loops is 0 was never
// executed (here the inner side of a nested loop whose outer produced no rows).
// PostgreSQL prints "(never executed)" rather than an actual-timing clause; joe
// previously always printed "(actual time=... loops=0)".
//
// PG-native: " ->  Seq Scan on public.cats c  (cost=...) (never executed)"
// joe (old): " ->  Seq Scan on public.cats c  (cost=...) (actual time=0.000..0.000 rows=0 loops=0)"
func TestRenderNeverExecuted(t *testing.T) {
	out := renderCorrectnessFixture(t, "never_executed")

	// Bind the clause to the specific zero-loops node and to the costs that precede
	// it, so "(never executed)" must replace the whole timing clause (not be appended
	// or land on another node) to pass.
	require.Contains(t, out, "Seq Scan on public.cats c  (cost=0.00..1.50 rows=50 width=4) (never executed)",
		"a node with Actual Loops=0 must render (never executed) in place of the timing clause, matching PostgreSQL")
	require.NotContains(t, out, "loops=0)",
		"a never-executed node must not render an actual-timing clause with loops=0")
}

// TestRenderTriggerConstraintGuard covers A3. PostgreSQL appends "for constraint
// <name>" only for constraint triggers; a plain trigger renders just
// "Trigger <name>:". joe hard-coded the "for constraint" clause, producing a
// dangling "for constraint :" for non-constraint triggers.
//
// PG-native: "Trigger mytrig: time=..." and
//
//	"Trigger RI_ConstraintTrigger_c_16505 for constraint trg_child_pid_fkey: time=..."
//
// joe (old): "Trigger mytrig for constraint : time=..."
func TestRenderTriggerConstraintGuard(t *testing.T) {
	out := renderCorrectnessFixture(t, "triggers")

	// Assert the full lines (through the time/calls values the fixture pins), so a
	// regression in the time/calls formatting is caught too, not just the clause.
	require.Contains(t, out, "Trigger mytrig: time=0.881 calls=1\n",
		"a non-constraint trigger must render without a for-constraint clause")
	require.Contains(t, out, "Trigger RI_ConstraintTrigger_c_16505 for constraint trg_child_pid_fkey: time=0.698 calls=1\n",
		"a constraint trigger must keep its for-constraint clause")
	require.NotContains(t, out, "for constraint : ",
		"a non-constraint trigger must not render a dangling \"for constraint :\"")
}

// TestRenderFunctionScanAlias covers A4. PostgreSQL schema-qualifies the function
// name (under VERBOSE) and omits the alias when it equals that name. joe rendered
// " on <FunctionName> <Alias>" unconditionally and without the schema, duplicating
// the name and dropping the qualification.
//
// PG-native: " Function Scan on pg_catalog.generate_series  (cost=...)"
// joe (old): " Function Scan on generate_series generate_series  (cost=...)"
func TestRenderFunctionScanAlias(t *testing.T) {
	out := renderCorrectnessFixture(t, "function_scan")

	require.Contains(t, out, "Function Scan on pg_catalog.generate_series  (cost=",
		"a Function Scan must schema-qualify and drop the alias when it equals the function name")
	require.NotContains(t, out, "generate_series generate_series",
		"a Function Scan must not duplicate the function name as a redundant alias")
}

// TestRenderFunctionScanDifferingAlias covers the other side of A4: when the alias
// differs from the function name (e.g. "select * from generate_series(1,10) gs(n)"),
// it must be kept (and the function name schema-qualified). This exercises the
// alias-append branch that the equal-alias fixture above does not.
//
// PG-native: " Function Scan on pg_catalog.generate_series gs  (cost=...)"
func TestRenderFunctionScanDifferingAlias(t *testing.T) {
	out := renderCorrectnessFixture(t, "function_scan_alias")

	require.Contains(t, out, "Function Scan on pg_catalog.generate_series gs  (cost=",
		"a Function Scan must schema-qualify the function name and keep a differing alias")
}

// TestRenderTempBuffers covers A5. An on-disk sort reports temp block I/O.
// PostgreSQL renders a "temp read=N written=N" section in the Buffers line and
// comma-separates it from the shared section. joe's per-node Buffers builder
// handled only shared and local buffers (dropping temp) and joined sections with
// a plain space instead of a comma.
//
// PG-native: "   Buffers: shared hit=323, temp read=608 written=666"
// joe (old): "   Buffers: shared hit=323"
func TestRenderTempBuffers(t *testing.T) {
	out := renderCorrectnessFixture(t, "sort_temp_buffers")

	// Assert the full line, including the comma between sections, so the
	// inter-section separator is guarded (not just the temp counters).
	require.Contains(t, out, "Buffers: shared hit=323, temp read=608 written=666",
		"an on-disk sort must render comma-separated shared and temp buffer sections, matching PostgreSQL")
}

// TestRenderLocalBuffers covers the local Buffers section and the shared->local
// comma separator. Buffer counts accumulate up the subtree, so a join whose outer
// side scans a regular table (shared blocks) and whose inner side scans a temp
// table (local blocks) reports both on the join node. The fixture is a real pg16
// "select r.id from reg_buf r join tmp_buf t on r.id = t.id" capture.
//
// PG-native: "   Buffers: shared hit=29, local hit=8"
func TestRenderLocalBuffers(t *testing.T) {
	out := renderCorrectnessFixture(t, "local_buffers")

	// The join node carries both sections: guards the local label and the
	// shared->local comma in one assertion.
	require.Contains(t, out, "Buffers: shared hit=29, local hit=8",
		"a node touching both shared and local blocks must render comma-separated shared and local sections, matching PostgreSQL")
	// A node with only local blocks (the inner temp Seq Scan) renders the bare
	// local section, without a leading comma.
	require.Contains(t, out, "Buffers: local hit=8",
		"a local-only node must render the local section alone, matching PostgreSQL")
}

// TestRenderBitmapIndexScanOn covers A6. A Bitmap Index Scan has no table, so
// PostgreSQL renders "Bitmap Index Scan on <index>", not the "using <index>" form
// used by Index/Index Only Scans. joe appended "using" for any node with an index.
//
// PG-native: " ->  Bitmap Index Scan on idx_items_cat  (cost=...)"
// joe (old): " ->  Bitmap Index Scan using idx_items_cat  (cost=...)"
func TestRenderBitmapIndexScanOn(t *testing.T) {
	out := renderCorrectnessFixture(t, "bitmap_index_scan")

	require.Contains(t, out, "Bitmap Index Scan on idx_items_cat  (cost=",
		"a Bitmap Index Scan must render \"on <index>\", matching PostgreSQL")
	require.NotContains(t, out, "Bitmap Index Scan using",
		"a Bitmap Index Scan must not render the \"using <index>\" form")
}

// TestAggregateNodeType pins the Aggregate caption for every strategy and partial
// mode, covering the Mixed->MixedAggregate and the Partial/Finalize branches that
// the fixture-based tests above don't all reach (the goldens carry only
// Plain/Sorted/Hashed).
func TestAggregateNodeType(t *testing.T) {
	cases := []struct {
		strategy    string
		partialMode string
		want        string
	}{
		{"", "", "Aggregate"},
		{"Plain", "Simple", "Aggregate"},
		{"Sorted", "Simple", "GroupAggregate"},
		{"Hashed", "Simple", "HashAggregate"},
		{"Mixed", "Simple", "MixedAggregate"},
		{"Plain", "Partial", "Partial Aggregate"},
		{"Sorted", "Finalize", "Finalize GroupAggregate"},
		{"Hashed", "Partial", "Partial HashAggregate"},
		{"Mixed", "Finalize", "Finalize MixedAggregate"},
	}

	for _, c := range cases {
		t.Run(c.strategy+"/"+c.partialMode, func(t *testing.T) {
			got := aggregateNodeType(&Plan{Strategy: c.strategy, PartialMode: c.partialMode})
			require.Equal(t, c.want, got, "strategy %q, partial mode %q", c.strategy, c.partialMode)
		})
	}
}

// TestScanTarget pins the shared relation/CTE/function scan caption suffix,
// including the alias-drop, no-schema, and empty-name (no clause) branches. The
// empty-name case guards against a stray " on" for a Function Scan whose function
// name is absent (e.g. a multi-function ROWS FROM).
func TestScanTarget(t *testing.T) {
	cases := []struct {
		name   string
		schema string
		object string
		alias  string
		want   string
	}{
		{"schema-qualified, redundant alias dropped", "pg_catalog", "generate_series", "generate_series", " on pg_catalog.generate_series"},
		{"schema-qualified, distinct alias kept", "pg_catalog", "generate_series", "gs", " on pg_catalog.generate_series gs"},
		{"no schema, redundant alias dropped", "", "t_items", "t_items", " on t_items"},
		{"no schema, distinct alias kept", "", "t_items", "i", " on t_items i"},
		{"empty name yields no clause", "public", "", "x", ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, scanTarget(c.schema, c.object, c.alias))
		})
	}
}

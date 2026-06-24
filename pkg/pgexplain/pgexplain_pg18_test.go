/*
2026 © Postgres.ai
*/

package pgexplain

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// PostgreSQL 18 reports fractional, two-decimal "Actual Rows" (averaged over
// loops) even for whole counts, e.g. "Actual Rows": 5.00 and 0.50. Before the
// uint64->float64 fix, json.Decode rejects the decimal and NewExplain fails for
// every PG18 EXPLAIN ANALYZE. This fixture reproduces a nested loop whose inner
// node averages to 0.50 rows/loop.
const inputJSONPostgres18FractionalRows = `[
  {
    "Plan": {
      "Node Type": "Nested Loop",
      "Parallel Aware": false,
      "Join Type": "Inner",
      "Startup Cost": 0.15,
      "Total Cost": 12.30,
      "Plan Rows": 5,
      "Plan Width": 8,
      "Actual Startup Time": 0.020,
      "Actual Total Time": 0.030,
      "Actual Rows": 5.00,
      "Actual Loops": 1,
      "Disabled": false,
      "Plans": [
        {
          "Node Type": "Seq Scan",
          "Parent Relationship": "Outer",
          "Parallel Aware": false,
          "Relation Name": "p",
          "Alias": "p",
          "Startup Cost": 0.00,
          "Total Cost": 1.10,
          "Plan Rows": 10,
          "Plan Width": 4,
          "Actual Startup Time": 0.005,
          "Actual Total Time": 0.007,
          "Actual Rows": 10.00,
          "Actual Loops": 1,
          "Disabled": false
        },
        {
          "Node Type": "Index Only Scan",
          "Parent Relationship": "Inner",
          "Parallel Aware": false,
          "Scan Direction": "Forward",
          "Index Name": "c_pid_idx",
          "Relation Name": "c",
          "Alias": "c",
          "Startup Cost": 0.00,
          "Total Cost": 1.12,
          "Plan Rows": 1,
          "Plan Width": 4,
          "Actual Startup Time": 0.001,
          "Actual Total Time": 0.001,
          "Actual Rows": 0.50,
          "Actual Loops": 10,
          "Index Cond": "(pid = p.id)",
          "Heap Fetches": 0,
          "Index Searches": 1,
          "Disabled": false
        }
      ]
    },
    "Planning Time": 0.100,
    "Triggers": [],
    "Execution Time": 0.050
  }
]`

func TestRenderPostgres18FractionalRows(t *testing.T) {
	explain, err := NewExplain(inputJSONPostgres18FractionalRows)
	require.NoError(t, err) // before fix: cannot unmarshal number 5.00 into Go ... uint64

	out := explain.RenderPlanText()

	// Whole counts render as integers (back-compat with pre-18 output);
	// fractional averages render with two decimals (matching PG18).
	require.Contains(t, out, "rows=5 loops=1", "whole actual rows should render as an integer")
	require.Contains(t, out, "rows=10 loops=1", "whole actual rows should render as an integer")
	require.Contains(t, out, "rows=0.50 loops=10", "fractional actual rows should render with two decimals")
	require.NotContains(t, out, "rows=5.00", "whole actual rows must not gain a decimal suffix")
}

// inputJSONPostgres18DetailFields carries PG18 per-node additions: Disabled (only
// the Seq Scan is disabled), Index Searches, WAL Buffers Full, and Storage on a
// Materialize node.
const inputJSONPostgres18DetailFields = `[
  {
    "Plan": {
      "Node Type": "Aggregate", "Strategy": "Plain", "Parallel Aware": false,
      "Startup Cost": 10.00, "Total Cost": 10.01, "Plan Rows": 1, "Plan Width": 8,
      "Actual Startup Time": 0.5, "Actual Total Time": 0.5, "Actual Rows": 1.00, "Actual Loops": 1,
      "Disabled": false,
      "WAL Records": 6, "WAL FPI": 0, "WAL Bytes": 369, "WAL Buffers Full": 2,
      "Plans": [
        {
          "Node Type": "Materialize", "Parent Relationship": "Outer", "Parallel Aware": false,
          "Startup Cost": 0.0, "Total Cost": 1.0, "Plan Rows": 5, "Plan Width": 4,
          "Actual Startup Time": 0.1, "Actual Total Time": 0.2, "Actual Rows": 5.00, "Actual Loops": 1,
          "Storage": "Memory", "Maximum Storage": 17,
          "Plans": [
            {
              "Node Type": "Seq Scan", "Parent Relationship": "Outer", "Parallel Aware": false,
              "Relation Name": "noidx", "Alias": "noidx",
              "Startup Cost": 0.0, "Total Cost": 1.0, "Plan Rows": 5, "Plan Width": 4,
              "Actual Startup Time": 0.01, "Actual Total Time": 0.02, "Actual Rows": 5.00, "Actual Loops": 1,
              "Disabled": true
            }
          ]
        },
        {
          "Node Type": "Index Only Scan", "Parent Relationship": "Inner", "Parallel Aware": false,
          "Scan Direction": "Forward", "Index Name": "c_pid_idx", "Relation Name": "c", "Alias": "c",
          "Startup Cost": 0.0, "Total Cost": 1.0, "Plan Rows": 1, "Plan Width": 4,
          "Actual Startup Time": 0.001, "Actual Total Time": 0.001, "Actual Rows": 1.00, "Actual Loops": 1,
          "Heap Fetches": 0, "Index Searches": 3
        }
      ]
    },
    "Planning Time": 0.1, "Triggers": [], "Execution Time": 0.6
  }
]`

func TestRenderPostgres18DetailFields(t *testing.T) {
	explain, err := NewExplain(inputJSONPostgres18DetailFields)
	require.NoError(t, err)

	out := explain.RenderPlanText()

	require.Contains(t, out, "Disabled: true", "the disabled Seq Scan should be annotated")
	require.Equal(t, 1, strings.Count(out, "Disabled: true"), "only genuinely disabled nodes are annotated")
	require.Contains(t, out, "Index Searches: 3")
	require.Contains(t, out, "Storage: Memory  Maximum Storage: 17kB")
	require.Contains(t, out, "WAL: records=6 fpi=0 bytes=369 buffers-full=2")
}

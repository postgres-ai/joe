/*
2026 © Postgres.ai
*/

package main

import (
	"bytes"
	"strings"
	"testing"
)

// seqScanJSON is a minimal EXPLAIN (ANALYZE, FORMAT JSON) document for a single
// Seq Scan node, carrying the cost/rows/width and actual timing fields plus the
// top-level Planning/Execution timings that RenderStats reports on.
const seqScanJSON = `[
  {
    "Plan": {
      "Node Type": "Seq Scan",
      "Relation Name": "t_items",
      "Schema": "joecap",
      "Alias": "t_items",
      "Startup Cost": 0.00,
      "Total Cost": 9.25,
      "Plan Rows": 200,
      "Plan Width": 8,
      "Actual Startup Time": 0.006,
      "Actual Total Time": 0.036,
      "Actual Rows": 200,
      "Actual Loops": 1,
      "Filter": "(t_items.val > 5)",
      "Rows Removed by Filter": 300,
      "Shared Hit Blocks": 3
    },
    "Planning Time": 0.462,
    "Execution Time": 0.199
  }
]`

func TestRender(t *testing.T) {
	t.Run("plan", func(t *testing.T) {
		var buf bytes.Buffer
		if err := render(strings.NewReader(seqScanJSON), &buf, false); err != nil {
			t.Fatalf("render returned an error: %v", err)
		}

		out := buf.String()
		for _, want := range []string{"Seq Scan on", "joecap.t_items", "cost=", "rows="} {
			if !strings.Contains(out, want) {
				t.Errorf("plan output missing %q\n--- output ---\n%s", want, out)
			}
		}

		if strings.Contains(out, statsSeparator) {
			t.Errorf("plan-only output should not include the stats separator\n--- output ---\n%s", out)
		}
	})

	t.Run("stats", func(t *testing.T) {
		var buf bytes.Buffer
		if err := render(strings.NewReader(seqScanJSON), &buf, true); err != nil {
			t.Fatalf("render returned an error: %v", err)
		}

		out := buf.String()
		if !strings.Contains(out, "Seq Scan on") {
			t.Errorf("stats output missing the plan line\n--- output ---\n%s", out)
		}
		if !strings.Contains(out, statsSeparator) {
			t.Errorf("stats output missing the stats separator %q\n--- output ---\n%s", statsSeparator, out)
		}
		if !strings.Contains(out, "Time:") {
			t.Errorf("stats output missing the timing block\n--- output ---\n%s", out)
		}
	})

	t.Run("parse error", func(t *testing.T) {
		var buf bytes.Buffer
		if err := render(strings.NewReader("{ this is not valid explain json"), &buf, false); err == nil {
			t.Error("render should return an error for malformed JSON, got nil")
		}
	})
}

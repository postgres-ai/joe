/*
2026 © Postgres.ai
*/

package main

import (
	"bytes"
	"os"
	"path/filepath"
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
		// Pin values parsed straight from the fixture so the assertions tie the
		// output to the input (a renderer emitting a fixed string with these
		// substrings but ignoring the JSON would fail here).
		for _, want := range []string{
			"Seq Scan on",
			"joecap.t_items",
			"cost=0.00..9.25",
			"rows=200",
			"width=8",
			"Filter: (t_items.val > 5)",
		} {
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
		// Pin timing values from the fixture's Planning/Execution Time fields.
		for _, want := range []string{"planning: 0.462 ms", "execution: 0.199 ms"} {
			if !strings.Contains(out, want) {
				t.Errorf("stats output missing %q\n--- output ---\n%s", want, out)
			}
		}
	})

	t.Run("parse error", func(t *testing.T) {
		var buf bytes.Buffer
		err := render(strings.NewReader("{ this is not valid explain json"), &buf, false)
		if err == nil {
			t.Fatal("render should return an error for malformed JSON, got nil")
		}
		// The error must stay attributed to the JSON parsing step so the message
		// remains clear through future refactors.
		if !strings.Contains(err.Error(), "parse EXPLAIN JSON") {
			t.Errorf("error should mention parsing EXPLAIN JSON, got: %v", err)
		}
	})

	// NewExplain rejects empty/whitespace input (a JSON decode error) and an
	// empty array (an "Empty explain" error); render must surface an error for
	// each without panicking.
	t.Run("invalid input", func(t *testing.T) {
		for name, input := range map[string]string{
			"empty":       "",
			"whitespace":  "   \n\t ",
			"empty array": "[]",
		} {
			t.Run(name, func(t *testing.T) {
				var buf bytes.Buffer
				if err := render(strings.NewReader(input), &buf, false); err == nil {
					t.Errorf("render(%q) should return an error, got nil", input)
				}
			})
		}
	})
}

// TestRun covers the file-path branch of run(): the happy path reading from a
// real file, and the error path for a path that does not exist. (Stdin is left
// to render's tests, which exercise the same rendering code through an io.Reader.)
func TestRun(t *testing.T) {
	t.Run("file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "plan.json")
		if err := os.WriteFile(path, []byte(seqScanJSON), 0o600); err != nil {
			t.Fatalf("failed to write fixture file: %v", err)
		}

		var buf bytes.Buffer
		if err := run(path, &buf, false); err != nil {
			t.Fatalf("run returned an error: %v", err)
		}

		if out := buf.String(); !strings.Contains(out, "Seq Scan on") {
			t.Errorf("run output missing the plan line\n--- output ---\n%s", out)
		}
	})

	t.Run("missing file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "does-not-exist.json")

		var buf bytes.Buffer
		err := run(path, &buf, false)
		if err == nil {
			t.Fatal("run should return an error for a nonexistent file, got nil")
		}
		if !strings.Contains(err.Error(), "failed to open") {
			t.Errorf("error should mention failing to open the file, got: %v", err)
		}
	})
}

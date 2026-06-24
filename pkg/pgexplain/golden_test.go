/*
2026 © Postgres.ai
*/

package pgexplain

import (
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// goldenUpdate, when set, (re)writes the golden .txt files instead of asserting
// against them. Run: go test ./pkg/pgexplain/ -run TestGolden -update
var goldenUpdate = flag.Bool("update", false, "regenerate pkg/pgexplain golden files from the testdata JSON fixtures")

// goldenSeparator delimits the rendered plan text from the rendered stats text
// inside a single golden file.
const goldenSeparator = "\n--- stats ---\n"

// goldenFractionalRows matches an actual-timing rows token whose value carries a
// decimal point, e.g. "actual time=0.001..0.001 rows=0.40 loops=5". PostgreSQL 18+
// reports per-loop-averaged Actual Rows, which can be fractional.
var goldenFractionalRows = regexp.MustCompile(`actual[^)]*\brows=\d+\.\d+`)

// goldenIntegerRows matches an actual-timing rows token with a whole value,
// e.g. "actual time=0.016..0.019 rows=5 loops=1".
var goldenIntegerRows = regexp.MustCompile(`actual[^)]*\brows=\d+\b(?:\.\d+)?`)

// goldenSettingsLine matches the rendered "Settings:" line. The settings are
// rendered from a Go map, so their order is non-deterministic across runs;
// goldenCanonicalize sorts the entries to keep golden comparisons stable.
var goldenSettingsLine = regexp.MustCompile(`(?m)^Settings: (.+)$`)

// goldenRender parses a captured EXPLAIN JSON fixture and renders the combined
// plan-and-stats text that the golden file stores.
func goldenRender(t *testing.T, jsonPath string) string {
	t.Helper()

	raw, err := os.ReadFile(jsonPath)
	require.NoError(t, err, "read fixture %s", jsonPath)

	// NewExplain succeeding is the key regression guard: PostgreSQL 18 serializes
	// "Actual Rows" as a float (e.g. 5.00 / 0.40), which previously broke decoding.
	explain, err := NewExplain(string(raw))
	require.NoError(t, err, "NewExplain failed for %s (PG18 fractional Actual Rows regression?)", jsonPath)

	out := explain.RenderPlanText() + goldenSeparator + explain.RenderStats()

	return goldenCanonicalize(out)
}

// goldenCanonicalize makes rendered output deterministic by sorting the entries
// of the map-rendered "Settings:" line, whose order otherwise varies per run.
func goldenCanonicalize(out string) string {
	return goldenSettingsLine.ReplaceAllStringFunc(out, func(line string) string {
		const prefix = "Settings: "
		entries := strings.Split(strings.TrimPrefix(line, prefix), ", ")
		sort.Strings(entries)

		return prefix + strings.Join(entries, ", ")
	})
}

// goldenPathFor maps testdata/<pgNN>/<name>.json to testdata/golden/<pgNN>/<name>.txt.
func goldenPathFor(jsonPath string) string {
	version := filepath.Base(filepath.Dir(jsonPath))
	name := strings.TrimSuffix(filepath.Base(jsonPath), filepath.Ext(jsonPath))

	return filepath.Join("testdata", "golden", version, name+".txt")
}

// goldenName builds a readable sub-test name like "pg18/nested_loop".
func goldenName(jsonPath string) string {
	version := filepath.Base(filepath.Dir(jsonPath))
	name := strings.TrimSuffix(filepath.Base(jsonPath), filepath.Ext(jsonPath))

	return version + "/" + name
}

func TestGolden(t *testing.T) {
	fixtures, err := filepath.Glob("testdata/pg*/*.json")
	require.NoError(t, err)
	require.NotEmpty(t, fixtures, "no testdata/pg*/*.json fixtures found")

	for _, jsonPath := range fixtures {
		jsonPath := jsonPath

		t.Run(goldenName(jsonPath), func(t *testing.T) {
			out := goldenRender(t, jsonPath)
			goldenPath := goldenPathFor(jsonPath)

			if *goldenUpdate {
				require.NoError(t, os.MkdirAll(filepath.Dir(goldenPath), 0o755))
				require.NoError(t, os.WriteFile(goldenPath, []byte(out), 0o644))

				return
			}

			want, err := os.ReadFile(goldenPath)
			require.NoError(t, err, "missing golden %s; re-run: go test ./pkg/pgexplain/ -run TestGolden -update", goldenPath)
			require.Equal(t, string(want), out,
				"rendered output differs from golden %s; if this change is intended re-run: go test ./pkg/pgexplain/ -run TestGolden -update", goldenPath)
		})
	}
}

// TestGoldenPG18FractionalRows targets the PostgreSQL 18 fractional Actual Rows
// behaviour directly, independent of the golden snapshots.
func TestGoldenPG18FractionalRows(t *testing.T) {
	const fixture = "testdata/pg18/nested_loop.json"

	if _, err := os.Stat(fixture); err != nil {
		t.Skipf("fixture %s missing, skipping", fixture)
	}

	text := goldenRender(t, fixture)

	require.Regexp(t, goldenFractionalRows, text,
		"expected a fractional actual rows token (e.g. rows=0.40) in %s", fixture)
	require.Regexp(t, goldenIntegerRows, text,
		"expected an integer actual rows token (e.g. rows=5) in %s", fixture)
}

// TestGoldenPG18Insert checks that a PostgreSQL 18 ModifyTable/Insert plan renders
// the "Insert on <table>" caption.
func TestGoldenPG18Insert(t *testing.T) {
	const fixture = "testdata/pg18/insert.json"

	if _, err := os.Stat(fixture); err != nil {
		t.Skipf("fixture %s missing, skipping", fixture)
	}

	text := goldenRender(t, fixture)

	require.Contains(t, text, "Insert on", "expected an \"Insert on\" caption in %s", fixture)
}

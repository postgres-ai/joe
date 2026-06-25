/*
2026 © Postgres.ai
*/

// Command explainrender converts a PostgreSQL `EXPLAIN (FORMAT JSON)` document
// into PostgreSQL's standard text plan (and optionally a stats summary) using
// joe's pkg/pgexplain renderer. joe collects plans as JSON but often needs to
// show the familiar text plan, and neither psql nor PostgreSQL can convert an
// existing JSON plan back to text — so pkg/pgexplain performs that translation.
//
// It is handy for debugging that JSON->text translation and for diffing joe's
// output against PostgreSQL's own text EXPLAIN across server versions; any
// difference is a rendering-fidelity bug.
//
// Usage:
//
//	explainrender [-stats] [file]
//
// The input (a file path argument, or stdin when omitted) must be the output of
// EXPLAIN (... FORMAT JSON), e.g. EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) ....
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"gitlab.com/postgres-ai/joe/pkg/pgexplain"
)

const usage = `explainrender converts a PostgreSQL EXPLAIN (FORMAT JSON) document into
PostgreSQL's standard text plan (and optionally a stats summary) using joe's
pkg/pgexplain renderer. psql cannot convert an existing JSON plan back to text;
joe receives plans as JSON, so pkg/pgexplain re-renders the standard text form.

Useful for debugging that JSON->text translation and for diffing joe's output
against PostgreSQL's own text EXPLAIN across versions (any difference is a bug).

Usage:
  explainrender [-stats] [file]

The input must be the output of EXPLAIN (... FORMAT JSON), for example:
  EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) SELECT ...

With no file argument, the JSON is read from stdin.

Flags:
`

// statsSeparator delimits the rendered plan from the rendered stats summary in
// the combined output produced with -stats.
const statsSeparator = "\n===== STATS =====\n"

// render reads an EXPLAIN (FORMAT JSON) document from in, renders it with joe's
// pgexplain renderer, and writes the text plan to out. When withStats is true it
// also appends the stats summary under statsSeparator.
func render(in io.Reader, out io.Writer, withStats bool) error {
	data, err := io.ReadAll(in)
	if err != nil {
		return fmt.Errorf("failed to read input: %w", err)
	}

	ex, err := pgexplain.NewExplain(string(data))
	if err != nil {
		return fmt.Errorf("failed to parse EXPLAIN JSON: %w", err)
	}

	if _, err := io.WriteString(out, ex.RenderPlanText()); err != nil {
		return fmt.Errorf("failed to write plan: %w", err)
	}

	if withStats {
		if _, err := io.WriteString(out, statsSeparator); err != nil {
			return fmt.Errorf("failed to write stats separator: %w", err)
		}

		if _, err := io.WriteString(out, ex.RenderStats()); err != nil {
			return fmt.Errorf("failed to write stats: %w", err)
		}
	}

	return nil
}

// run resolves the input source (the given file path, or stdin when path is
// empty) and hands it to render. It is split out from main so that a deferred
// file close runs before main decides on the process exit code.
func run(path string, out io.Writer, withStats bool) error {
	var in io.Reader = os.Stdin

	if path != "" {
		f, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("failed to open %s: %w", path, err)
		}

		defer func() { _ = f.Close() }()

		in = f
	}

	return render(in, out, withStats)
}

func main() {
	flag.Usage = func() {
		_, _ = fmt.Fprint(flag.CommandLine.Output(), usage)

		flag.PrintDefaults()
	}

	withStats := flag.Bool("stats", false, "also render joe's stats summary after the plan")

	flag.Parse()

	if err := run(flag.Arg(0), os.Stdout, *withStats); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "error:", err)

		os.Exit(1)
	}
}

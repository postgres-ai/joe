# explainrender

`explainrender` converts a PostgreSQL `EXPLAIN (FORMAT JSON)` document into
**PostgreSQL's standard text plan** (and, optionally, a stats summary) using
Joe's [`pkg/pgexplain`](../../pkg/pgexplain) renderer.

This is a plain JSON→text translation, not a Joe-specific format: the output is
meant to match what `EXPLAIN` (without `FORMAT JSON`) prints. Joe needs it
because it receives plans as JSON (it wants the structured form), but often
wants to show the familiar text plan — and **neither psql nor PostgreSQL can
convert an existing JSON plan back to text**, so `pkg/pgexplain` does it.

It is a small development/debugging helper, not part of the bot runtime.

## Why

- **Debug the JSON→text translation** in isolation, without standing up the full bot.
- **Diff Joe's output against PostgreSQL's native text `EXPLAIN`** for the same
  query (see below). Because the goal is to reproduce the standard text plan,
  **any difference is a rendering-fidelity bug** — and running it across
  PostgreSQL versions surfaces version-specific regressions.

## Usage

```
explainrender [-stats] [file]
```

- Reads the EXPLAIN JSON from `file` if given, otherwise from stdin.
- Prints the rendered text plan. With `-stats`, also prints the stats summary
  under a `===== STATS =====` separator.
- Flags must precede the file argument (`explainrender -stats file.json`): Go's
  `flag` package stops parsing at the first non-flag argument, so anything after
  the file is treated as an extra positional. Passing more than one positional
  argument prints usage and exits non-zero.
- `-h` prints usage. On a parse failure it prints a clear message to stderr and
  exits non-zero.

The input must be the output of `EXPLAIN (... FORMAT JSON)`, e.g.
`EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) SELECT ...`.

### Examples

From stdin:

```bash
docker exec pgv18 psql -U postgres -d bench -tAc \
  "explain (analyze, costs, verbose, buffers, format json, settings, wal) \
   select count(*) from items where val > 500" \
  | go run ./cmd/explainrender -stats
```

From a file:

```bash
go run ./cmd/explainrender path/to/plan.json
```

Build a standalone binary:

```bash
make build-explainrender   # -> bin/explainrender
```

Using the built binary:

```bash
./bin/explainrender -stats < plan.json
```

## Diffing against PostgreSQL's native text EXPLAIN

The main use case: render the *same* query as both JSON (through `pkg/pgexplain`)
and native text (through PostgreSQL), then diff. Since the renderer is supposed
to reproduce PostgreSQL's standard text plan, any difference points at a
JSON→text rendering bug, and running it across PG versions surfaces
version-specific regressions.

```bash
PG=pgv18
Q="select count(*) from items where val > 500"

# pkg/pgexplain's rendering of the JSON plan:
docker exec $PG psql -U postgres -d bench -tAc \
  "explain (analyze, costs, verbose, buffers, settings, wal, format json) $Q" \
  | go run ./cmd/explainrender > /tmp/joe.txt

# PostgreSQL's native text plan:
docker exec $PG psql -U postgres -d bench -tAc \
  "explain (analyze, costs, verbose, buffers, settings, wal) $Q" \
  > /tmp/pg.txt

diff /tmp/pg.txt /tmp/joe.txt
```

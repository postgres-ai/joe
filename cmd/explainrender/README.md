# explainrender

`explainrender` renders a PostgreSQL `EXPLAIN (FORMAT JSON)` document into Joe's
text plan (and, optionally, Joe's stats summary) using Joe's own
[`pkg/pgexplain`](../../pkg/pgexplain) renderer — the same code path Joe uses
when replying in chat.

It is a small development/debugging helper, not part of the bot runtime.

## Why

- **Debug Joe's JSON→text rendering** in isolation, without standing up the full bot.
- **Diff Joe's output against PostgreSQL's native text `EXPLAIN`** for the same
  query, to catch rendering regressions across PostgreSQL versions (see below).

## Usage

```
explainrender [-stats] [file]
```

- Reads the EXPLAIN JSON from `file` if given, otherwise from stdin.
- Prints the rendered text plan. With `-stats`, also prints Joe's stats summary
  under a `===== STATS =====` separator.
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

## Diffing against PostgreSQL's native text EXPLAIN

The main use case: render the *same* query as both JSON (through Joe) and native
text (through PostgreSQL), then diff. Differences point at JSON→text rendering
bugs, and running it across PG versions surfaces version-specific regressions.

```bash
PG=pgv18
Q="select count(*) from items where val > 500"

# Joe's rendering of the JSON plan:
docker exec $PG psql -U postgres -d bench -tAc \
  "explain (analyze, costs, verbose, buffers, settings, wal, format json) $Q" \
  | go run ./cmd/explainrender > /tmp/joe.txt

# PostgreSQL's native text plan:
docker exec $PG psql -U postgres -d bench -tAc \
  "explain (analyze, costs, verbose, buffers, settings, wal) $Q" \
  > /tmp/pg.txt

diff /tmp/pg.txt /tmp/joe.txt
```

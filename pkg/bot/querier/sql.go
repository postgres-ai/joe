/*
2019 © Postgres.ai
*/

package querier

import (
	"bytes"
	"context"
	"strings"

	"github.com/jackc/pgconn"
	"github.com/jackc/pgtype/pgxtype"
	"github.com/olekukonko/tablewriter"
	"github.com/pkg/errors"
	"gitlab.com/postgres-ai/database-lab/v3/pkg/log"
)

const (
	// SyntaxPQErrorCode defines the pq syntax error code.
	SyntaxPQErrorCode = "42601"

	// SystemPQErrorCodeUndefinedFile defines external errors to PostgreSQL itself.
	SystemPQErrorCodeUndefinedFile = "58P01"
)

// DBQuery runs query and returns table results.
func DBQuery(ctx context.Context, db pgxtype.Querier, query string, args ...interface{}) ([][]string, error) {
	return runTableQuery(ctx, db, query, args...)
}

// DBQueryWithResponse runs query with returning results.
func DBQueryWithResponse(ctx context.Context, db pgxtype.Querier, query string) (string, error) {
	return runQuery(ctx, db, query)
}

const observeQuery = `with data as (
  select
    row_number() over()::text as "#",
    c.relkind,
    c.relnamespace::regnamespace,
    c.relname,
    format(
      '%s%s',
      nullif(c.relnamespace::regnamespace::text, 'public') || '.',
      coalesce(ind.tablename, relname)
    ) as belongs_to_relation, -- todo: do the same for TOAST tables/indexes?
    l.locktype,
    l.mode,
    l.granted::text,
    l.fastpath::text
  from pg_locks as l
  join pg_class as c on c.oid = l.relation
  left join pg_indexes as ind on
    c.relkind = 'i'
    and indexname = c.relname
    and schemaname = c.relnamespace::regnamespace::name
  where l.pid = $1
)
select *
from data
where
  belongs_to_relation not in ( -- not a perfect solution: <query> can also work with them
    'pg_catalog.pg_class',
    'pg_catalog.pg_indexes',
    'pg_catalog.pg_index',
    'pg_catalog.pg_locks',
    'pg_catalog.pg_namespace',
    'pg_catalog.pg_tablespace'
  )
order by
  belongs_to_relation,
  case relkind when 'r' then 0 when 'v' then 1 when 'i' then 9 else 5 end;`

// ObserveLocks selects locks details filtered by pid.
func ObserveLocks(ctx context.Context, db pgxtype.Querier, pid int) ([][]string, error) {
	return runTableQuery(ctx, db, observeQuery, pid)
}

func runQuery(ctx context.Context, db pgxtype.Querier, query string) (string, error) {
	log.Dbg("DB query:", query)

	// TODO(anatoly): Retry mechanic.
	var result = ""

	rows, err := db.Query(ctx, query)
	if err != nil {
		log.Err("DB query:", err)
		return "", clarifyQueryError([]byte(query), err)
	}
	defer rows.Close()

	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			log.Err("DB query traversal:", err)
			return s, err
		}

		result += s + "\n"
	}

	if err := rows.Err(); err != nil {
		log.Err("DB query traversal:", err)
		return result, err
	}

	return result, nil
}

// runTableQuery runs query and returns results in the table view.
func runTableQuery(ctx context.Context, db pgxtype.Querier, query string, args ...interface{}) ([][]string, error) {
	log.Dbg("DB table query:", query)

	rows, err := db.Query(ctx, query, args...)
	if err != nil {
		log.Err("DB query:", err)
		return nil, clarifyQueryError([]byte(query), err)
	}
	defer rows.Close()

	// Prepare a result table.
	resultTable := make([][]string, 0)
	head := make([]string, 0)

	for rows.Next() {
		if len(head) == 0 {
			// We have to get the descriptions of fields after rows.Next only https://github.com/jackc/pgx/issues/459
			fieldDescriptions := rows.FieldDescriptions()
			for _, column := range fieldDescriptions {
				head = append(head, string(column.Name))
			}

			resultTable = append(resultTable, head)
		}

		rawValues := rows.RawValues()
		resultRow := make([]string, 0, len(head))

		for _, rawValue := range rawValues {
			resultRow = append(resultRow, string(rawValue))
		}

		resultTable = append(resultTable, resultRow)
	}

	if err := rows.Err(); err != nil {
		log.Err("DB query traversal:", err)
		return resultTable, err
	}

	return resultTable, nil
}

// RenderTable renders table result in the psql style.
func RenderTable(tableString *strings.Builder, res [][]string) {
	tableString.Write([]byte("```"))
	defer tableString.Write([]byte("```"))

	if len(res) == 0 {
		tableString.WriteString("No results.\n")
		return
	}

	table := tablewriter.NewWriter(tableString)
	table.SetBorder(false)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetHeader(res[0])
	table.AppendBulk(res[1:])
	table.Render()
}

func clarifyQueryError(query []byte, err error) error {
	if err == nil {
		return err
	}

	switch queryErr := err.(type) {
	case *pgconn.PgError:
		switch queryErr.Code {
		case SyntaxPQErrorCode:
			// Check &nbsp; - ASCII code 160
			if bytes.Contains(query, []byte{160}) {
				return errors.WithMessage(err,
					`There are "non-breaking spaces" in your input (ASCII code 160). Repeat your request using regular spaces instead (ASCII code 32).`)
			}
		default:
			return err
		}
	}

	return err
}

// GetBackendPID returns backend pid.
func GetBackendPID(ctx context.Context, conn pgxtype.Querier) (int, error) {
	var backendPID int

	if err := conn.QueryRow(ctx, `select pg_backend_pid()`).Scan(&backendPID); err != nil {
		return backendPID, err
	}

	return backendPID, nil
}

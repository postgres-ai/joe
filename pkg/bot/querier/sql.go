/*
2019 © Postgres.ai
*/

package querier

import (
	"bytes"
	"context"
	"strings"

	"github.com/jackc/pgconn"
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/olekukonko/tablewriter"
	"github.com/pkg/errors"
	"gitlab.com/postgres-ai/database-lab/pkg/log"
)

const (
	// SyntaxPQErrorCode defines the pq syntax error code.
	SyntaxPQErrorCode = "42601"

	// SystemPQErrorCodeUndefinedFile defines external errors to PostgreSQL itself.
	SystemPQErrorCodeUndefinedFile = "58P01"
)

// DBQuery runs query and returns table results.
func DBQuery(ctx context.Context, db *pgxpool.Pool, query string, args ...interface{}) ([][]string, error) {
	return runTableQuery(ctx, db, query, args...)
}

// DBQueryWithResponse runs query with returning results.
func DBQueryWithResponse(db *pgxpool.Pool, query string) (string, error) {
	return runQuery(context.TODO(), db, query)
}

func runQuery(ctx context.Context, db *pgxpool.Pool, query string) (string, error) {
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
func runTableQuery(ctx context.Context, db *pgxpool.Pool, query string, args ...interface{}) ([][]string, error) {
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

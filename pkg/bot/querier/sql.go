/*
2019 © Postgres.ai
*/

package querier

import (
	"bytes"
	"database/sql"
	"strings"

	"github.com/lib/pq"
	"github.com/olekukonko/tablewriter"
	"github.com/pkg/errors"
	"gitlab.com/postgres-ai/database-lab/pkg/log"
)

const (
	QueryExplain        = "EXPLAIN (FORMAT TEXT) "
	QueryExplainAnalyze = "EXPLAIN (ANALYZE, COSTS, VERBOSE, BUFFERS, FORMAT JSON) "
)

const (
	// SyntaxPQErrorCode defines the pq syntax error code.
	SyntaxPQErrorCode = "42601"

	// SystemPQErrorCodeUndefinedFile defines external errors to PostgreSQL itself.
	SystemPQErrorCodeUndefinedFile = "58P01"
)

// DBExec runs query without returning results.
func DBExec(db *sql.DB, query string) error {
	_, err := runQuery(db, query, true)
	return err
}

// DBQuery runs query and returns table results.
func DBQuery(db *sql.DB, query string, args ...interface{}) ([][]string, error) {
	return runTableQuery(db, query, args...)
}

func DBExplain(db *sql.DB, query string) (string, error) {
	return runQuery(db, QueryExplain+query, false)
}

func DBExplainAnalyze(db *sql.DB, query string) (string, error) {
	return runQuery(db, QueryExplainAnalyze+query, false)
}

func runQuery(db *sql.DB, query string, omitResp bool, args ...interface{}) (string, error) {
	log.Dbg("DB query:", query)

	// TODO(anatoly): Retry mechanic.
	var result = ""

	rows, err := db.Query(query, args...)
	if err != nil {
		log.Err("DB query:", err)
		return "", clarifyQueryError([]byte(query), err)
	}
	defer rows.Close()

	if !omitResp {
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
	}

	return result, nil
}

// runTableQuery runs query and returns results in the table view.
func runTableQuery(db *sql.DB, query string, args ...interface{}) ([][]string, error) {
	log.Dbg("DB table query:", query)

	rows, err := db.Query(query, args...)
	if err != nil {
		log.Err("DB query:", err)
		return nil, clarifyQueryError([]byte(query), err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		log.Err("Failed to get columns:", err)
		return nil, errors.Wrap(err, "failed to read column names")
	}

	// Prepare a result table.
	resultTable := [][]string{columns}

	row := make([]string, len(columns))
	scanInterfaces := make([]interface{}, len(columns))

	for i := range scanInterfaces {
		scanInterfaces[i] = &row[i]
	}

	for rows.Next() {
		if err := rows.Scan(scanInterfaces...); err != nil {
			log.Err("DB query traversal:", err)
			return nil, err
		}
		resultTable = append(resultTable, row)
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
	case *pq.Error:
		switch queryErr.Code {
		case SyntaxPQErrorCode:
			// Check &nbsp; - ASCII code 160
			if bytes.Contains(query, []byte{160}) {
				return errors.WithMessage(err,
					`There are "non-breaking spaces" in your input (ASCII code 160). Please edit your request and use regular spaces only (ASCII code 32).`)
			}
		default:
			return err
		}
	}

	return err
}

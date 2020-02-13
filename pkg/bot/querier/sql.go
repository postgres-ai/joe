/*
2019 © Postgres.ai
*/

package querier

import (
	"bytes"
	"database/sql"

	"github.com/lib/pq"
	"github.com/pkg/errors"
	"gitlab.com/postgres-ai/database-lab/pkg/log"
)

const (
	QueryExplain        = "EXPLAIN (FORMAT TEXT) "
	QueryExplainAnalyze = "EXPLAIN (ANALYZE, COSTS, VERBOSE, BUFFERS, FORMAT JSON) "
)

// SyntaxPQErrorCode defines the pq syntax error code.
const SyntaxPQErrorCode = "42601"

func DBExec(db *sql.DB, query string) error {
	_, err := runQuery(db, query, true)
	return err
}

func DBExplain(db *sql.DB, query string) (string, error) {
	return runQuery(db, QueryExplain+query, false)
}

func DBExplainAnalyze(db *sql.DB, query string) (string, error) {
	return runQuery(db, QueryExplainAnalyze+query, false)
}

func runQuery(db *sql.DB, query string, omitResp bool) (string, error) {
	log.Dbg("DB query:", query)

	// TODO(anatoly): Retry mechanic.
	var result = ""

	rows, err := db.Query(query)
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
					`There are "non-breaking spaces" in your input (ACSII code 160). Please edit your request and use regular spaces only (ASCII code 32).`)
			}
		default:
			return err
		}
	}

	return err
}

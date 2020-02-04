/*
2019 © Postgres.ai
*/

package querier

import (
	"database/sql"

	"gitlab.com/postgres-ai/database-lab/pkg/log"
)

const QUERY_EXPLAIN = "EXPLAIN (FORMAT TEXT) "
const QUERY_EXPLAIN_ANALYZE = "EXPLAIN (ANALYZE, COSTS, VERBOSE, BUFFERS, FORMAT JSON) "

func DBExec(db *sql.DB, query string) error {
	_, err := runQuery(db, query, true)
	return err
}

func DBExplain(db *sql.DB, query string) (string, error) {
	return runQuery(db, QUERY_EXPLAIN+query, false)
}

func DBExplainAnalyze(db *sql.DB, query string) (string, error) {
	return runQuery(db, QUERY_EXPLAIN_ANALYZE+query, false)
}

func runQuery(db *sql.DB, query string, omitResp bool) (string, error) {
	log.Dbg("DB query:", query)

	// TODO(anatoly): Retry mechanic.
	var result = ""

	rows, err := db.Query(query)
	if err != nil {
		log.Err("DB query:", err)
		return "", err
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

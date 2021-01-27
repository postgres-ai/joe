/*
2021 © Postgres.ai
*/

// Package operator contains operator helpers.
package operator

import (
	"gitlab.com/postgres-ai/database-lab/pkg/util"
)

var (
	hintExplainDmlWords = []string{"insert", "select", "update", "delete", "with"}
	hintExecDdlWords    = []string{"alter", "create", "drop", "set"}
)

// IsDML checks if the query is related to data manipulation.
func IsDML(command string) bool {
	return util.Contains(hintExplainDmlWords, command)
}

// IsDDL checks if the query is related to data definition.
func IsDDL(command string) bool {
	return util.Contains(hintExecDdlWords, command)
}

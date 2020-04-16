/*
2019 © Postgres.ai
*/

package msgproc

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParsingMessage(t *testing.T) {
	testCases := []struct {
		caseName        string
		incomingMessage string
		expectedCommand string
		expectedQuery   string
	}{
		{
			caseName:        "single command",
			incomingMessage: "activity",
			expectedCommand: "activity",
			expectedQuery:   "",
		},
		{
			caseName:        "simple explain",
			incomingMessage: "explain select 1",
			expectedCommand: "explain",
			expectedQuery:   "select 1",
		},
		{
			caseName:        "multibyte encoding",
			incomingMessage: "хлеб бородинский",
			expectedCommand: "хлеб",
			expectedQuery:   "бородинский",
		},
		{
			caseName:        "simple explain with spaces",
			incomingMessage: "explain     select    1",
			expectedCommand: "explain",
			expectedQuery:   "select    1",
		},
		{
			caseName:        "psql",
			incomingMessage: "\\d+",
			expectedCommand: "\\d+",
			expectedQuery:   "",
		},
		{
			caseName: "multiline explain", incomingMessage: `explain 
select 1`,
			expectedCommand: "explain",
			expectedQuery:   "select 1",
		},
		{
			caseName: "multiline explain with a comment in the end", incomingMessage: `explain 
select 1 -- just multiline`,
			expectedCommand: "explain",
			expectedQuery:   "select 1 -- just multiline",
		},
		{
			caseName: "multiline explain with a comment in the middle", incomingMessage: `explain 
select from pgbench_accounts -- just multiline
  where bid = 100`,
			expectedCommand: "explain",
			expectedQuery: `select from pgbench_accounts -- just multiline
  where bid = 100`,
		},
	}

	for _, tc := range testCases {
		t.Log(tc.caseName)

		command, query := parseIncomingMessage(tc.incomingMessage)

		assert.Equal(t, tc.expectedCommand, command)
		assert.Equal(t, tc.expectedQuery, query)
	}
}

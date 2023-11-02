package command

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAnalyzePrefix(t *testing.T) {
	testCases := []struct {
		input          int
		expectedOutput string
	}{
		{
			input:          0,
			expectedOutput: "EXPLAIN (ANALYZE, COSTS, VERBOSE, BUFFERS, FORMAT JSON ) ",
		},
		{
			input:          90600,
			expectedOutput: "EXPLAIN (ANALYZE, COSTS, VERBOSE, BUFFERS, FORMAT JSON ) ",
		},
		{
			input:          120000,
			expectedOutput: "EXPLAIN (ANALYZE, COSTS, VERBOSE, BUFFERS, FORMAT JSON , SETTINGS TRUE) ",
		},
		{
			input:          160000,
			expectedOutput: "EXPLAIN (ANALYZE, COSTS, VERBOSE, BUFFERS, FORMAT JSON , SETTINGS TRUE) ",
		},
	}

	for _, tc := range testCases {
		output := analyzePrefix(tc.input)
		assert.Equal(t, tc.expectedOutput, output)
	}
}

package slackrtm

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUnfurlLinks(t *testing.T) {
	testCases := []struct {
		input          string
		expectedOutput string
	}{
		{
			input:          "EXPLAIN (FORMAT TEXT) select <http://t1.id|t1.id> from t1;",
			expectedOutput: "EXPLAIN (FORMAT TEXT) select t1.id from t1;",
		},
		{
			input:          "select <mailto:'john@gmail.com|'john_doe123@gmail.com>';",
			expectedOutput: "select 'john_doe123@gmail.com';",
		},
	}

	for _, tc := range testCases {
		output := unfurlLinks(tc.input)
		assert.Equal(t, tc.expectedOutput, output)
	}
}

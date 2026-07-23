package command

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPlanPrefix(t *testing.T) {
	testCases := []struct {
		name           string
		dbVersion      int
		expectedPrefix string
	}{
		{
			name:           "unknown version",
			dbVersion:      0,
			expectedPrefix: queryExplain,
		},
		{
			name:           "PostgreSQL 15",
			dbVersion:      150000,
			expectedPrefix: queryExplain,
		},
		{
			name:           "PostgreSQL 16",
			dbVersion:      160000,
			expectedPrefix: "EXPLAIN (GENERIC_PLAN, FORMAT TEXT) ",
		},
		{
			name:           "PostgreSQL 19 beta",
			dbVersion:      190000,
			expectedPrefix: "EXPLAIN (GENERIC_PLAN, FORMAT TEXT) ",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expectedPrefix, planPrefix(tc.dbVersion))
		})
	}
}

package command

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"gitlab.com/postgres-ai/joe/pkg/services/platform"
)

func TestPlanPrefix(t *testing.T) {
	testCases := []struct {
		name           string
		generic        bool
		expectedPrefix string
	}{
		{
			name:           "normal plan",
			expectedPrefix: queryExplain,
		},
		{
			name:           "generic plan",
			generic:        true,
			expectedPrefix: queryGenericPlan,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := PlanCmd{generic: tc.generic}
			assert.Equal(t, tc.expectedPrefix, cmd.planPrefix())
		})
	}
}

func TestGenericPlanRequiresPostgres16(t *testing.T) {
	cmd := NewGenericPlan(
		&platform.Command{Query: "select * from t where id = $1"},
		nil,
		nil,
		150000,
		nil,
	)

	err := cmd.Execute(context.Background())
	assert.EqualError(t, err, MsgGenericPlanVersionReq)
}

/*
2024 © Postgres.ai
*/

package pgexplain

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// invariantFloat64 dereferences a *float64 for use in assertions.
// It panics if the pointer is nil — callers should guard with a nil check.
func invariantFloat64(p *float64) float64 {
	if p == nil {
		panic("unexpected nil *float64")
	}

	return *p
}

func TestNormalizeIOTiming(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		inputJSON         string
		wantReadNil       bool
		wantWriteNil      bool
		wantRead          float64
		wantWrite         float64
		wantChildReadNil  bool
		wantChildRead     float64
		checkChild        bool
		parentShouldBeNil bool
		wantStatsNA       bool
	}{
		{
			name: "legacy keys only",
			inputJSON: `[{
				"Plan": {
					"Node Type": "Seq Scan",
					"Actual Rows": 1,
					"Actual Loops": 1,
					"I/O Read Time": 7.0,
					"I/O Write Time": 3.0
				},
				"Planning Time": 0.1,
				"Execution Time": 0.2
			}]`,
			wantRead:  7.0,
			wantWrite: 3.0,
		},
		{
			name: "PG17 split keys only",
			inputJSON: `[{
				"Plan": {
					"Node Type": "Seq Scan",
					"Actual Rows": 1,
					"Actual Loops": 1,
					"Shared I/O Read Time": 5.0,
					"Temp I/O Read Time": 2.0,
					"Shared I/O Write Time": 1.0
				},
				"Planning Time": 0.1,
				"Execution Time": 0.2
			}]`,
			wantRead:  7.0,
			wantWrite: 1.0,
		},
		{
			name: "mixed nested: parent no i/o keys, child has split keys",
			inputJSON: `[{
				"Plan": {
					"Node Type": "Limit",
					"Actual Rows": 1,
					"Actual Loops": 1,
					"Plans": [{
						"Node Type": "Seq Scan",
						"Actual Rows": 1,
						"Actual Loops": 1,
						"Shared I/O Read Time": 3.0,
						"Temp I/O Read Time": 1.5,
						"Shared I/O Write Time": 0.5
					}]
				},
				"Planning Time": 0.1,
				"Execution Time": 0.2
			}]`,
			wantReadNil:       true,
			wantWriteNil:      true,
			parentShouldBeNil: true,
			checkChild:        true,
			wantChildRead:     4.5,
		},
		{
			name: "neither present",
			inputJSON: `[{
				"Plan": {
					"Node Type": "Seq Scan",
					"Actual Rows": 1,
					"Actual Loops": 1
				},
				"Planning Time": 0.1,
				"Execution Time": 0.2
			}]`,
			wantReadNil:  true,
			wantWriteNil: true,
			wantStatsNA:  true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ex, err := NewExplain(tc.inputJSON)
			require.NoError(t, err)
			require.NotNil(t, ex)

			if tc.parentShouldBeNil {
				require.Nil(t, ex.IOReadTime, "parent IOReadTime should be nil")
				require.Nil(t, ex.IOWriteTime, "parent IOWriteTime should be nil")
			} else {
				if tc.wantReadNil {
					require.Nil(t, ex.IOReadTime)
				} else {
					require.NotNil(t, ex.IOReadTime)
					require.Equal(t, tc.wantRead, invariantFloat64(ex.IOReadTime))
				}

				if tc.wantWriteNil {
					require.Nil(t, ex.IOWriteTime)
				} else {
					require.NotNil(t, ex.IOWriteTime)
					require.Equal(t, tc.wantWrite, invariantFloat64(ex.IOWriteTime))
				}
			}

			if tc.checkChild {
				require.NotEmpty(t, ex.Plan.Plans, "expected at least one child plan")
				child := &ex.Plan.Plans[0]

				if tc.wantChildReadNil {
					require.Nil(t, child.IOReadTime)
				} else {
					require.NotNil(t, child.IOReadTime)
					require.Equal(t, tc.wantChildRead, invariantFloat64(child.IOReadTime))
				}
			}

			if tc.wantStatsNA {
				stats := ex.RenderStats()
				require.True(t, strings.Contains(stats, "I/O read: N/A"),
					"expected 'I/O read: N/A' in stats output, got:\n%s", stats)
			}
		})
	}
}

func TestUnknownKeysAndNodeTypeDoNotPanic(t *testing.T) {
	t.Parallel()

	input := `[{
		"Plan": {
			"Node Type": "Quantum Scan",
			"Actual Rows": 3.50,
			"Actual Loops": 2,
			"Plan Rows": 5,
			"Plan Width": 8,
			"Startup Cost": 0.00,
			"Total Cost": 1.00,
			"Actual Startup Time": 0.001,
			"Actual Total Time": 0.010,
			"Unknown Top Level Key": "ignored",
			"Another Unknown": 42
		},
		"Unknown Outer Key": "also ignored",
		"Planning Time": 0.05,
		"Execution Time": 0.10
	}]`

	ex, err := NewExplain(input)
	require.NoError(t, err)
	require.NotNil(t, ex)

	var planText string

	require.NotPanics(t, func() {
		planText = ex.RenderPlanText()
	})

	require.NotPanics(t, func() {
		_ = ex.RenderStats()
	})

	require.True(t, strings.Contains(planText, "Quantum Scan"),
		"expected 'Quantum Scan' in plan text, got:\n%s", planText)

	// A fractional Actual Rows (PG18) on an unknown node type must render too.
	require.True(t, strings.Contains(planText, "rows=3.50"),
		"expected 'rows=3.50' in plan text, got:\n%s", planText)
}

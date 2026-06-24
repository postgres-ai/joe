/*
2026 © Postgres.ai
*/

package pgexplain

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestModifyTableRendersOperation guards the fix to the ModifyTable constant: the
// real "Node Type" PostgreSQL emits is "ModifyTable" (no space), and joe shows the
// DML verb (Insert/Update/Delete) from the Operation field rather than the raw node
// name. Previously the constant was "Modify Table", so the special case never fired.
func TestModifyTableRendersOperation(t *testing.T) {
	const insert = `[
  {
    "Plan": {
      "Node Type": "ModifyTable",
      "Operation": "Insert",
      "Relation Name": "c",
      "Alias": "c",
      "Startup Cost": 0.00, "Total Cost": 0.01, "Plan Rows": 1, "Plan Width": 4,
      "Actual Startup Time": 0.02, "Actual Total Time": 0.02, "Actual Rows": 0.00, "Actual Loops": 1,
      "Plans": [
        {
          "Node Type": "Result", "Parent Relationship": "Outer", "Parallel Aware": false,
          "Startup Cost": 0.00, "Total Cost": 0.01, "Plan Rows": 1, "Plan Width": 4,
          "Actual Startup Time": 0.001, "Actual Total Time": 0.001, "Actual Rows": 1.00, "Actual Loops": 1
        }
      ]
    },
    "Planning Time": 0.05, "Triggers": [], "Execution Time": 0.1
  }
]`

	explain, err := NewExplain(insert)
	require.NoError(t, err)

	out := explain.RenderPlanText()
	require.Contains(t, out, "Insert on c", "ModifyTable should render its Operation, not the raw node type")
	require.NotContains(t, out, "ModifyTable")
}

// TestModernNodeTypesRenderByName checks that node types joe does not special-case
// (added to PostgreSQL over the years) still render correctly via the generic
// branch — by their exact name — without error.
func TestModernNodeTypesRenderByName(t *testing.T) {
	for _, nodeType := range []string{"Gather Merge", "Incremental Sort", "Memoize", "WindowAgg"} {
		t.Run(nodeType, func(t *testing.T) {
			json := `[{"Plan":{"Node Type":"` + nodeType + `","Parallel Aware":false,` +
				`"Startup Cost":0.0,"Total Cost":1.0,"Plan Rows":1,"Plan Width":4,` +
				`"Actual Startup Time":0.0,"Actual Total Time":0.1,"Actual Rows":1.00,"Actual Loops":1}}]`

			explain, err := NewExplain(json)
			require.NoError(t, err)
			require.Contains(t, explain.RenderPlanText(), nodeType)
		})
	}
}

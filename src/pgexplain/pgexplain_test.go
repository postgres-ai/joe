/*
2019 © Postgres.ai
*/

package pgexplain

import (
	"testing"

	"../util"

	"github.com/sergi/go-diff/diffmatchpatch"
)

func TestVisualize(t *testing.T) {
	tests := []struct {
		inputJson string
		expected  string
	}{
		{
			inputJson: INPUT_JSON_0,
			expected:  EXPECTED_TEXT_0,
		},
		{
			inputJson: INPUT_JSON_1,
			expected:  EXPECTED_TEXT_1,
		},
		{
			inputJson: INPUT_JSON_2,
			expected:  EXPECTED_TEXT_2,
		},
		{
			inputJson: INPUT_JSON_3,
			expected:  EXPECTED_TEXT_3,
		},
		{
			inputJson: INPUT_JSON_4,
			expected:  EXPECTED_TEXT_4,
		},
	}

	for i, test := range tests {
		inputJson := test.inputJson
		expected := test.expected
		explainConfig := ExplainConfig{}

		explain, err := NewExplain(inputJson, explainConfig)
		if err != nil {
			t.Errorf("(%d) explain parsing failed: %v", i, err)
			t.FailNow()
		}

		actual := explain.RenderPlanText()

		if actual != expected {
			t.Errorf("(%d) got different than expected: \n%s\n", i, diff(expected, actual))
		}
	}
}

func TestTips(t *testing.T) {
	explainConfig := ExplainConfig{
		Params: ParamsConfig{
			BuffersHitReadSeqScan:         50,
			BuffersReadBigMax:             100,
			BuffersHitBigMax:              1000,
			AddLimitMinRows:               10000,
			TempWrittenBlocksMin:          0,
			IndexIneffHighFilteredMin:     100,
			VacuumAnalyzeNeededFetchesMin: 0,
		},
		Tips: []Tip{
			{
				Code: "SEQSCAN_USED",
			},
			{
				Code: "TOO_MUCH_DATA",
			},
			{
				Code: "ADD_LIMIT",
			},
			{
				Code: "TEMP_BUF_WRITTEN",
			},
			{
				Code: "INDEX_INEFFICIENT_HIGH_FILTERED",
			},
			{
				Code: "VACUUM_ANALYZE_NEEDED",
			},
		},
	}

	tests := []struct {
		inputJson     string
		expectedCodes []string
	}{
		// SEQSCAN_USED.
		{
			inputJson: `[
				{
					"Plan": {
						"Node Type": "Seq Scan",
						"Relation Name": "table_1",
						"Shared Hit Blocks": 0,
						"Shared Read Blocks": 0
					}
				}
			]`,
			expectedCodes: []string{},
		},
		{
			inputJson: `[
				{
					"Plan": {
						"Node Type": "Seq Scan",
						"Relation Name": "table_1",
						"Shared Hit Blocks": 40,
						"Shared Read Blocks": 20
					}
				}
			]`,
			expectedCodes: []string{"SEQSCAN_USED"},
		},
		// TOO_MUCH_DATA.
		{
			inputJson: `[
				{
					"Plan": {
						"Node Type": "Index Scan",
						"Relation Name": "table_1",
						"Shared Hit Blocks": 100000,
						"Shared Read Blocks": 100000
					}
				}
			]`,
			expectedCodes: []string{"TOO_MUCH_DATA"},
		},
		// ADD_LIMIT.
		{
			inputJson: `[
				{
					"Plan": {
						"Node Type": "Index Scan",
						"Relation Name": "table_1",
						"Actual Rows": 1000
					}
				}
			]`,
			expectedCodes: []string{},
		},
		{
			inputJson: `[
				{
					"Plan": {
						"Node Type": "Limit",
						"Relation Name": "table_1",
						"Actual Rows": 100000
					}
				}
			]`,
			expectedCodes: []string{},
		},
		{
			inputJson: `[
				{
					"Plan": {
						"Node Type": "Index Scan",
						"Relation Name": "table_1",
						"Actual Rows": 100000
					}
				}
			]`,
			expectedCodes: []string{"ADD_LIMIT"},
		},
		// TEMP_BUF_WRITTEN.
		{
			inputJson: `[
				{
					"Plan": {
						"Node Type": "Index Scan",
						"Temp Written Blocks": 100
					}
				}
			]`,
			expectedCodes: []string{"TEMP_BUF_WRITTEN"},
		},
		// INDEX_INEFFICIENT_HIGH_FILTERED.
		{
			inputJson: `[
				{
					"Plan": {
						"Node Type": "Index Scan",
						"Rows Removed by Filter": 101
					}
				}
			]`,
			expectedCodes: []string{"INDEX_INEFFICIENT_HIGH_FILTERED"},
		},
		// VACUUM_ANALYZE_NEEDED.
		{
			inputJson: `[
				{
					"Plan": {
						"Node Type": "Index Only Scan",
						"Heap Fetches": 1
					}
				}
			]`,
			expectedCodes: []string{"VACUUM_ANALYZE_NEEDED"},
		},
	}

	for i, test := range tests {
		inputJson := test.inputJson
		expectedCodes := test.expectedCodes

		explain, err := NewExplain(inputJson, explainConfig)
		if err != nil {
			t.Errorf("(%d) explain parsing failed: %v", i, err)
			t.FailNow()
		}

		actualTips, err := explain.GetTips()
		if err != nil {
			t.Errorf("(%d) tips discover failed: %v", i, err)
			t.FailNow()
		}

		actualCodes := getCodes(actualTips)

		if !util.EqualStringSlicesUnordered(actualCodes, expectedCodes) {
			t.Errorf("(%d) got different than expected: \nActual: %s\nExpected: %s\n",
				i, actualCodes, expectedCodes)
		}
	}
}

func getCodes(tips []Tip) []string {
	if len(tips) == 0 {
		return make([]string, 0)
	}

	codes := make([]string, len(tips))
	for i, tip := range tips {
		codes[i] = tip.Code
	}

	return codes
}

func diff(a string, b string) string {
	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain(a, b, false)
	return dmp.DiffPrettyText(diffs)
}

// Test cases from Postgres 9.6.
const INPUT_JSON_0 = `[{
  "Plan": {
   "Node Type": "Limit",
   "Parallel Aware": false,
   "Startup Cost": 0.00,
   "Total Cost": 452.37,
   "Plan Rows": 200,
   "Plan Width": 33,
   "Actual Startup Time": 0.071,
   "Actual Total Time": 6.723,
   "Actual Rows": 200,
   "Actual Loops": 1,
   "Shared Hit Blocks": 1,
   "Shared Read Blocks": 326,
   "Shared Dirtied Blocks": 0,
   "Shared Written Blocks": 0,
   "Local Hit Blocks": 0,
   "Local Read Blocks": 0,
   "Local Dirtied Blocks": 0,
   "Local Written Blocks": 0,
   "Temp Read Blocks": 0,
   "Temp Written Blocks": 0,
   "Plans": [
     {
       "Node Type": "Seq Scan",
       "Parent Relationship": "Outer",
       "Parallel Aware": false,
       "Relation Name": "persons",
       "Alias": "persons",
       "Startup Cost": 0.00,
       "Total Cost": 159040.41,
       "Plan Rows": 70315,
       "Plan Width": 33,
       "Actual Startup Time": 0.071,
       "Actual Total Time": 6.704,
       "Actual Rows": 200,
       "Actual Loops": 1,
       "Filter": "((tech)::text = 'scss'::text)",
       "Rows Removed by Filter": 40711,
       "Shared Hit Blocks": 1,
       "Shared Read Blocks": 326,
       "Shared Dirtied Blocks": 0,
       "Shared Written Blocks": 0,
       "Local Hit Blocks": 0,
       "Local Read Blocks": 0,
       "Local Dirtied Blocks": 0,
       "Local Written Blocks": 0,
       "Temp Read Blocks": 0,
       "Temp Written Blocks": 0
     }
   ]
  },
  "Planning Time": 27.727,
  "Triggers": [],
  "Execution Time": 6.834
}]`

const EXPECTED_TEXT_0 = ` Limit  (cost=0.00..452.37 rows=200 width=33) (actual time=0.071..6.723 rows=200 loops=1)
   Buffers: shared hit=1 read=326
   ->  Seq Scan on persons  (cost=0.00..159040.41 rows=70315 width=33) (actual time=0.071..6.704 rows=200 loops=1)
         Filter: ((tech)::text = 'scss'::text)
         Rows Removed by Filter: 40711
         Buffers: shared hit=1 read=326
`

const INPUT_JSON_1 = `[
   {
     "Plan": {
       "Node Type": "Unique",
       "Parallel Aware": false,
       "Startup Cost": 156506.25,
       "Total Cost": 156507.37,
       "Plan Rows": 225,
       "Plan Width": 149,
       "Actual Startup Time": 3684.547,
       "Actual Total Time": 3684.613,
       "Actual Rows": 204,
       "Actual Loops": 1,
       "Shared Hit Blocks": 43757,
       "Shared Read Blocks": 0,
       "Shared Dirtied Blocks": 0,
       "Shared Written Blocks": 0,
       "Local Hit Blocks": 0,
       "Local Read Blocks": 0,
       "Local Dirtied Blocks": 0,
       "Local Written Blocks": 0,
       "Temp Read Blocks": 0,
       "Temp Written Blocks": 0,
       "Plans": [
         {
           "Node Type": "Unique",
           "Parent Relationship": "InitPlan",
           "Subplan Name": "CTE table_5",
           "Parallel Aware": false,
           "Startup Cost": 111147.74,
           "Total Cost": 111155.68,
           "Plan Rows": 1588,
           "Plan Width": 4,
           "Actual Startup Time": 3649.950,
           "Actual Total Time": 3653.392,
           "Actual Rows": 204,
           "Actual Loops": 1,
           "Shared Hit Blocks": 37235,
           "Shared Read Blocks": 0,
           "Shared Dirtied Blocks": 0,
           "Shared Written Blocks": 0,
           "Local Hit Blocks": 0,
           "Local Read Blocks": 0,
           "Local Dirtied Blocks": 0,
           "Local Written Blocks": 0,
           "Temp Read Blocks": 0,
           "Temp Written Blocks": 0,
           "Plans": [
             {
               "Node Type": "Sort",
               "Parent Relationship": "Outer",
               "Parallel Aware": false,
               "Startup Cost": 111147.74,
               "Total Cost": 111151.71,
               "Plan Rows": 1588,
               "Plan Width": 4,
               "Actual Startup Time": 3649.949,
               "Actual Total Time": 3651.878,
               "Actual Rows": 17434,
               "Actual Loops": 1,
               "Sort Key": ["u_1.id"],
               "Sort Method": "quicksort",
               "Sort Space Used": 1586,
               "Sort Space Type": "Memory",
               "Shared Hit Blocks": 37235,
               "Shared Read Blocks": 0,
               "Shared Dirtied Blocks": 0,
               "Shared Written Blocks": 0,
               "Local Hit Blocks": 0,
               "Local Read Blocks": 0,
               "Local Dirtied Blocks": 0,
               "Local Written Blocks": 0,
               "Temp Read Blocks": 0,
               "Temp Written Blocks": 0,
               "Plans": [
                 {
                   "Node Type": "Hash Join",
                   "Parent Relationship": "Outer",
                   "Parallel Aware": false,
                   "Join Type": "Left",
                   "Startup Cost": 2465.41,
                   "Total Cost": 111063.32,
                   "Plan Rows": 1588,
                   "Plan Width": 4,
                   "Actual Startup Time": 55.487,
                   "Actual Total Time": 3645.938,
                   "Actual Rows": 17434,
                   "Actual Loops": 1,
                   "Hash Cond": "(u_1.id = j_1.s_id)",
                   "Filter": "((u_1.created_at >= (now() - '3 days'::interval)) OR (u_1.updated_at >= now()) OR (p_1.created_at >= (now() - '3 days'::interval)) OR (p_1.updated_at >= now()) OR (c_1.created_at >= (now() - '3 days'::interval)) OR (c_1.updated_at >= now()) OR (j_1.created_at >= (now() - '3 days'::interval)))",
                   "Rows Removed by Filter": 2264748,
                   "Shared Hit Blocks": 37235,
                   "Shared Read Blocks": 0,
                   "Shared Dirtied Blocks": 0,
                   "Shared Written Blocks": 0,
                   "Local Hit Blocks": 0,
                   "Local Read Blocks": 0,
                   "Local Dirtied Blocks": 0,
                   "Local Written Blocks": 0,
                   "Temp Read Blocks": 0,
                   "Temp Written Blocks": 0,
                   "Plans": [
                     {
                       "Node Type": "Hash Join",
                       "Parent Relationship": "Outer",
                       "Parallel Aware": false,
                       "Join Type": "Inner",
                       "Startup Cost": 2402.70,
                       "Total Cost": 92272.71,
                       "Plan Rows": 2271649,
                       "Plan Width": 52,
                       "Actual Startup Time": 28.106,
                       "Actual Total Time": 1195.591,
                       "Actual Rows": 2282182,
                       "Actual Loops": 1,
                       "Hash Cond": "(c_1.s_id = u_1.id)",
                       "Shared Hit Blocks": 37210,
                       "Shared Read Blocks": 0,
                       "Shared Dirtied Blocks": 0,
                       "Shared Written Blocks": 0,
                       "Local Hit Blocks": 0,
                       "Local Read Blocks": 0,
                       "Local Dirtied Blocks": 0,
                       "Local Written Blocks": 0,
                       "Temp Read Blocks": 0,
                       "Temp Written Blocks": 0,
                       "Plans": [
                         {
                           "Node Type": "Seq Scan",
                           "Parent Relationship": "Outer",
                           "Parallel Aware": false,
                           "Relation Name": "table_1",
                           "Alias": "c_1",
                           "Startup Cost": 0.00,
                           "Total Cost": 58660.93,
                           "Plan Rows": 2264693,
                           "Plan Width": 20,
                           "Actual Startup Time": 0.009,
                           "Actual Total Time": 204.214,
                           "Actual Rows": 2281919,
                           "Actual Loops": 1,
                           "Shared Hit Blocks": 36014,
                           "Shared Read Blocks": 0,
                           "Shared Dirtied Blocks": 0,
                           "Shared Written Blocks": 0,
                           "Local Hit Blocks": 0,
                           "Local Read Blocks": 0,
                           "Local Dirtied Blocks": 0,
                           "Local Written Blocks": 0,
                           "Temp Read Blocks": 0,
                           "Temp Written Blocks": 0
                         },
                         {
                           "Node Type": "Hash",
                           "Parent Relationship": "Inner",
                           "Parallel Aware": false,
                           "Startup Cost": 2145.52,
                           "Total Cost": 2145.52,
                           "Plan Rows": 20574,
                           "Plan Width": 40,
                           "Actual Startup Time": 28.029,
                           "Actual Total Time": 28.029,
                           "Actual Rows": 20698,
                           "Actual Loops": 1,
                           "Hash Buckets": 32768,
                           "Original Hash Buckets": 32768,
                           "Hash Batches": 1,
                           "Original Hash Batches": 1,
                           "Peak Memory Usage": 1874,
                           "Shared Hit Blocks": 1196,
                           "Shared Read Blocks": 0,
                           "Shared Dirtied Blocks": 0,
                           "Shared Written Blocks": 0,
                           "Local Hit Blocks": 0,
                           "Local Read Blocks": 0,
                           "Local Dirtied Blocks": 0,
                           "Local Written Blocks": 0,
                           "Temp Read Blocks": 0,
                           "Temp Written Blocks": 0,
                           "Plans": [
                             {
                               "Node Type": "Hash Join",
                               "Parent Relationship": "Outer",
                               "Parallel Aware": false,
                               "Join Type": "Inner",
                               "Startup Cost": 1148.50,
                               "Total Cost": 2145.52,
                               "Plan Rows": 20574,
                               "Plan Width": 40,
                               "Actual Startup Time": 8.890,
                               "Actual Total Time": 23.319,
                               "Actual Rows": 20698,
                               "Actual Loops": 1,
                               "Hash Cond": "(p_1.s_id = u_1.id)",
                               "Shared Hit Blocks": 1196,
                               "Shared Read Blocks": 0,
                               "Shared Dirtied Blocks": 0,
                               "Shared Written Blocks": 0,
                               "Local Hit Blocks": 0,
                               "Local Read Blocks": 0,
                               "Local Dirtied Blocks": 0,
                               "Local Written Blocks": 0,
                               "Temp Read Blocks": 0,
                               "Temp Written Blocks": 0,
                               "Plans": [
                                 {
                                   "Node Type": "Seq Scan",
                                   "Parent Relationship": "Outer",
                                   "Parallel Aware": false,
                                   "Relation Name": "table_2",
                                   "Alias": "p_1",
                                   "Startup Cost": 0.00,
                                   "Total Cost": 714.74,
                                   "Plan Rows": 20574,
                                   "Plan Width": 20,
                                   "Actual Startup Time": 0.005,
                                   "Actual Total Time": 6.016,
                                   "Actual Rows": 20698,
                                   "Actual Loops": 1,
                                   "Shared Hit Blocks": 509,
                                   "Shared Read Blocks": 0,
                                   "Shared Dirtied Blocks": 0,
                                   "Shared Written Blocks": 0,
                                   "Local Hit Blocks": 0,
                                   "Local Read Blocks": 0,
                                   "Local Dirtied Blocks": 0,
                                   "Local Written Blocks": 0,
                                   "Temp Read Blocks": 0,
                                   "Temp Written Blocks": 0
                                 },
                                 {
                                   "Node Type": "Hash",
                                   "Parent Relationship": "Inner",
                                   "Parallel Aware": false,
                                   "Startup Cost": 892.11,
                                   "Total Cost": 892.11,
                                   "Plan Rows": 20511,
                                   "Plan Width": 20,
                                   "Actual Startup Time": 8.802,
                                   "Actual Total Time": 8.802,
                                   "Actual Rows": 20697,
                                   "Actual Loops": 1,
                                   "Hash Buckets": 32768,
                                   "Original Hash Buckets": 32768,
                                   "Hash Batches": 1,
                                   "Original Hash Batches": 1,
                                   "Peak Memory Usage": 1388,
                                   "Shared Hit Blocks": 687,
                                   "Shared Read Blocks": 0,
                                   "Shared Dirtied Blocks": 0,
                                   "Shared Written Blocks": 0,
                                   "Local Hit Blocks": 0,
                                   "Local Read Blocks": 0,
                                   "Local Dirtied Blocks": 0,
                                   "Local Written Blocks": 0,
                                   "Temp Read Blocks": 0,
                                   "Temp Written Blocks": 0,
                                   "Plans": [
                                     {
                                       "Node Type": "Seq Scan",
                                       "Parent Relationship": "Outer",
                                       "Parallel Aware": false,
                                       "Relation Name": "table_3",
                                       "Alias": "u_1",
                                       "Startup Cost": 0.00,
                                       "Total Cost": 892.11,
                                       "Plan Rows": 20511,
                                       "Plan Width": 20,
                                       "Actual Startup Time": 0.007,
                                       "Actual Total Time": 5.432,
                                       "Actual Rows": 20697,
                                       "Actual Loops": 1,
                                       "Shared Hit Blocks": 687,
                                       "Shared Read Blocks": 0,
                                       "Shared Dirtied Blocks": 0,
                                       "Shared Written Blocks": 0,
                                       "Local Hit Blocks": 0,
                                       "Local Read Blocks": 0,
                                       "Local Dirtied Blocks": 0,
                                       "Local Written Blocks": 0,
                                       "Temp Read Blocks": 0,
                                       "Temp Written Blocks": 0
                                     }
                                   ]
                                 }
                               ]
                             }
                           ]
                         }
                       ]
                     },
                     {
                       "Node Type": "Hash",
                       "Parent Relationship": "Inner",
                       "Parallel Aware": false,
                       "Startup Cost": 41.76,
                       "Total Cost": 41.76,
                       "Plan Rows": 1676,
                       "Plan Width": 12,
                       "Actual Startup Time": 0.566,
                       "Actual Total Time": 0.566,
                       "Actual Rows": 1640,
                       "Actual Loops": 1,
                       "Hash Buckets": 2048,
                       "Original Hash Buckets": 2048,
                       "Hash Batches": 1,
                       "Original Hash Batches": 1,
                       "Peak Memory Usage": 93,
                       "Shared Hit Blocks": 25,
                       "Shared Read Blocks": 0,
                       "Shared Dirtied Blocks": 0,
                       "Shared Written Blocks": 0,
                       "Local Hit Blocks": 0,
                       "Local Read Blocks": 0,
                       "Local Dirtied Blocks": 0,
                       "Local Written Blocks": 0,
                       "Temp Read Blocks": 0,
                       "Temp Written Blocks": 0,
                       "Plans": [
                         {
                           "Node Type": "Seq Scan",
                           "Parent Relationship": "Outer",
                           "Parallel Aware": false,
                           "Relation Name": "table_4",
                           "Alias": "j_1",
                           "Startup Cost": 0.00,
                           "Total Cost": 41.76,
                           "Plan Rows": 1676,
                           "Plan Width": 12,
                           "Actual Startup Time": 0.014,
                           "Actual Total Time": 0.306,
                           "Actual Rows": 1640,
                           "Actual Loops": 1,
                           "Shared Hit Blocks": 25,
                           "Shared Read Blocks": 0,
                           "Shared Dirtied Blocks": 0,
                           "Shared Written Blocks": 0,
                           "Local Hit Blocks": 0,
                           "Local Read Blocks": 0,
                           "Local Dirtied Blocks": 0,
                           "Local Written Blocks": 0,
                           "Temp Read Blocks": 0,
                           "Temp Written Blocks": 0
                         }
                       ]
                     }
                   ]
                 }
               ]
             }
           ]
         },
         {
           "Node Type": "Aggregate",
           "Strategy": "Hashed",
           "Partial Mode": "Simple",
           "Parent Relationship": "InitPlan",
           "Subplan Name": "CTE table_6",
           "Parallel Aware": false,
           "Startup Cost": 43980.17,
           "Total Cost": 43982.17,
           "Plan Rows": 200,
           "Plan Width": 12,
           "Actual Startup Time": 3670.930,
           "Actual Total Time": 3671.011,
           "Actual Rows": 204,
           "Actual Loops": 1,
           "Group Key": ["u_2.id"],
           "Shared Hit Blocks": 40598,
           "Shared Read Blocks": 0,
           "Shared Dirtied Blocks": 0,
           "Shared Written Blocks": 0,
           "Local Hit Blocks": 0,
           "Local Read Blocks": 0,
           "Local Dirtied Blocks": 0,
           "Local Written Blocks": 0,
           "Temp Read Blocks": 0,
           "Temp Written Blocks": 0,
           "Plans": [
             {
               "Node Type": "Nested Loop",
               "Parent Relationship": "Outer",
               "Parallel Aware": false,
               "Join Type": "Inner",
               "Startup Cost": 0.43,
               "Total Cost": 42378.81,
               "Plan Rows": 320272,
               "Plan Width": 8,
               "Actual Startup Time": 3649.978,
               "Actual Total Time": 3666.324,
               "Actual Rows": 22180,
               "Actual Loops": 1,
               "Shared Hit Blocks": 40598,
               "Shared Read Blocks": 0,
               "Shared Dirtied Blocks": 0,
               "Shared Written Blocks": 0,
               "Local Hit Blocks": 0,
               "Local Read Blocks": 0,
               "Local Dirtied Blocks": 0,
               "Local Written Blocks": 0,
               "Temp Read Blocks": 0,
               "Temp Written Blocks": 0,
               "Plans": [
                 {
                   "Node Type": "CTE Scan",
                   "Parent Relationship": "Outer",
                   "Parallel Aware": false,
                   "CTE Name": "table_5",
                   "Alias": "u_2",
                   "Startup Cost": 0.00,
                   "Total Cost": 31.76,
                   "Plan Rows": 1588,
                   "Plan Width": 4,
                   "Actual Startup Time": 3649.952,
                   "Actual Total Time": 3653.435,
                   "Actual Rows": 204,
                   "Actual Loops": 1,
                   "Shared Hit Blocks": 37235,
                   "Shared Read Blocks": 0,
                   "Shared Dirtied Blocks": 0,
                   "Shared Written Blocks": 0,
                   "Local Hit Blocks": 0,
                   "Local Read Blocks": 0,
                   "Local Dirtied Blocks": 0,
                   "Local Written Blocks": 0,
                   "Temp Read Blocks": 0,
                   "Temp Written Blocks": 0
                 },
                 {
                   "Node Type": "Index Scan",
                   "Parent Relationship": "Inner",
                   "Parallel Aware": false,
                   "Scan Direction": "Forward",
                   "Index Name": "table_1_s_id",
                   "Relation Name": "table_1",
                   "Alias": "c_2",
                   "Startup Cost": 0.43,
                   "Total Cost": 24.65,
                   "Plan Rows": 202,
                   "Plan Width": 8,
                   "Actual Startup Time": 0.008,
                   "Actual Total Time": 0.038,
                   "Actual Rows": 109,
                   "Actual Loops": 204,
                   "Index Cond": "(s_id = u_2.id)",
                   "Rows Removed by Index Recheck": 0,
                   "Shared Hit Blocks": 3363,
                   "Shared Read Blocks": 0,
                   "Shared Dirtied Blocks": 0,
                   "Shared Written Blocks": 0,
                   "Local Hit Blocks": 0,
                   "Local Read Blocks": 0,
                   "Local Dirtied Blocks": 0,
                   "Local Written Blocks": 0,
                   "Temp Read Blocks": 0,
                   "Temp Written Blocks": 0
                 }
               ]
             }
           ]
         },
         {
           "Node Type": "Sort",
           "Parent Relationship": "Outer",
           "Parallel Aware": false,
           "Startup Cost": 1368.40,
           "Total Cost": 1368.96,
           "Plan Rows": 225,
           "Plan Width": 149,
           "Actual Startup Time": 3684.546,
           "Actual Total Time": 3684.579,
           "Actual Rows": 208,
           "Actual Loops": 1,
           "Sort Key": ["u.id"],
           "Sort Method": "quicksort",
           "Sort Space Used": 63,
           "Sort Space Type": "Memory",
           "Shared Hit Blocks": 43757,
           "Shared Read Blocks": 0,
           "Shared Dirtied Blocks": 0,
           "Shared Written Blocks": 0,
           "Local Hit Blocks": 0,
           "Local Read Blocks": 0,
           "Local Dirtied Blocks": 0,
           "Local Written Blocks": 0,
           "Temp Read Blocks": 0,
           "Temp Written Blocks": 0,
           "Plans": [
             {
               "Node Type": "Nested Loop",
               "Parent Relationship": "Outer",
               "Parallel Aware": false,
               "Join Type": "Left",
               "Startup Cost": 973.20,
               "Total Cost": 1359.61,
               "Plan Rows": 225,
               "Plan Width": 149,
               "Actual Startup Time": 3680.276,
               "Actual Total Time": 3684.426,
               "Actual Rows": 208,
               "Actual Loops": 1,
               "Shared Hit Blocks": 43757,
               "Shared Read Blocks": 0,
               "Shared Dirtied Blocks": 0,
               "Shared Written Blocks": 0,
               "Local Hit Blocks": 0,
               "Local Read Blocks": 0,
               "Local Dirtied Blocks": 0,
               "Local Written Blocks": 0,
               "Temp Read Blocks": 0,
               "Temp Written Blocks": 0,
               "Plans": [
                 {
                   "Node Type": "Nested Loop",
                   "Parent Relationship": "Outer",
                   "Parallel Aware": false,
                   "Join Type": "Left",
                   "Startup Cost": 972.91,
                   "Total Cost": 1291.89,
                   "Plan Rows": 197,
                   "Plan Width": 145,
                   "Actual Startup Time": 3680.263,
                   "Actual Total Time": 3683.407,
                   "Actual Rows": 208,
                   "Actual Loops": 1,
                   "Shared Hit Blocks": 43133,
                   "Shared Read Blocks": 0,
                   "Shared Dirtied Blocks": 0,
                   "Shared Written Blocks": 0,
                   "Local Hit Blocks": 0,
                   "Local Read Blocks": 0,
                   "Local Dirtied Blocks": 0,
                   "Local Written Blocks": 0,
                   "Temp Read Blocks": 0,
                   "Temp Written Blocks": 0,
                   "Plans": [
                     {
                       "Node Type": "Nested Loop",
                       "Parent Relationship": "Outer",
                       "Parallel Aware": false,
                       "Join Type": "Inner",
                       "Startup Cost": 972.63,
                       "Total Cost": 1231.49,
                       "Plan Rows": 197,
                       "Plan Width": 74,
                       "Actual Startup Time": 3680.251,
                       "Actual Total Time": 3682.971,
                       "Actual Rows": 208,
                       "Actual Loops": 1,
                       "Shared Hit Blocks": 42645,
                       "Shared Read Blocks": 0,
                       "Shared Dirtied Blocks": 0,
                       "Shared Written Blocks": 0,
                       "Local Hit Blocks": 0,
                       "Local Read Blocks": 0,
                       "Local Dirtied Blocks": 0,
                       "Local Written Blocks": 0,
                       "Temp Read Blocks": 0,
                       "Temp Written Blocks": 0,
                       "Plans": [
                         {
                           "Node Type": "Nested Loop",
                           "Parent Relationship": "Outer",
                           "Parallel Aware": false,
                           "Join Type": "Inner",
                           "Startup Cost": 972.34,
                           "Total Cost": 1228.65,
                           "Plan Rows": 8,
                           "Plan Width": 72,
                           "Actual Startup Time": 3680.236,
                           "Actual Total Time": 3682.113,
                           "Actual Rows": 208,
                           "Actual Loops": 1,
                           "Shared Hit Blocks": 42019,
                           "Shared Read Blocks": 0,
                           "Shared Dirtied Blocks": 0,
                           "Shared Written Blocks": 0,
                           "Local Hit Blocks": 0,
                           "Local Read Blocks": 0,
                           "Local Dirtied Blocks": 0,
                           "Local Written Blocks": 0,
                           "Temp Read Blocks": 0,
                           "Temp Written Blocks": 0,
                           "Plans": [
                             {
                               "Node Type": "Hash Join",
                               "Parent Relationship": "Outer",
                               "Parallel Aware": false,
                               "Join Type": "Inner",
                               "Startup Cost": 971.91,
                               "Total Cost": 978.91,
                               "Plan Rows": 200,
                               "Plan Width": 44,
                               "Actual Startup Time": 3680.214,
                               "Actual Total Time": 3680.529,
                               "Actual Rows": 204,
                               "Actual Loops": 1,
                               "Hash Cond": "(c.s_id = p.s_id)",
                               "Shared Hit Blocks": 41107,
                               "Shared Read Blocks": 0,
                               "Shared Dirtied Blocks": 0,
                               "Shared Written Blocks": 0,
                               "Local Hit Blocks": 0,
                               "Local Read Blocks": 0,
                               "Local Dirtied Blocks": 0,
                               "Local Written Blocks": 0,
                               "Temp Read Blocks": 0,
                               "Temp Written Blocks": 0,
                               "Plans": [
                                 {
                                   "Node Type": "CTE Scan",
                                   "Parent Relationship": "Outer",
                                   "Parallel Aware": false,
                                   "CTE Name": "table_6",
                                   "Alias": "c",
                                   "Startup Cost": 0.00,
                                   "Total Cost": 4.00,
                                   "Plan Rows": 200,
                                   "Plan Width": 12,
                                   "Actual Startup Time": 3670.933,
                                   "Actual Total Time": 3671.064,
                                   "Actual Rows": 204,
                                   "Actual Loops": 1,
                                   "Shared Hit Blocks": 40598,
                                   "Shared Read Blocks": 0,
                                   "Shared Dirtied Blocks": 0,
                                   "Shared Written Blocks": 0,
                                   "Local Hit Blocks": 0,
                                   "Local Read Blocks": 0,
                                   "Local Dirtied Blocks": 0,
                                   "Local Written Blocks": 0,
                                   "Temp Read Blocks": 0,
                                   "Temp Written Blocks": 0
                                 },
                                 {
                                   "Node Type": "Hash",
                                   "Parent Relationship": "Inner",
                                   "Parallel Aware": false,
                                   "Startup Cost": 714.74,
                                   "Total Cost": 714.74,
                                   "Plan Rows": 20574,
                                   "Plan Width": 32,
                                   "Actual Startup Time": 9.210,
                                   "Actual Total Time": 9.210,
                                   "Actual Rows": 20698,
                                   "Actual Loops": 1,
                                   "Hash Buckets": 32768,
                                   "Original Hash Buckets": 32768,
                                   "Hash Batches": 1,
                                   "Original Hash Batches": 1,
                                   "Peak Memory Usage": 1473,
                                   "Shared Hit Blocks": 509,
                                   "Shared Read Blocks": 0,
                                   "Shared Dirtied Blocks": 0,
                                   "Shared Written Blocks": 0,
                                   "Local Hit Blocks": 0,
                                   "Local Read Blocks": 0,
                                   "Local Dirtied Blocks": 0,
                                   "Local Written Blocks": 0,
                                   "Temp Read Blocks": 0,
                                   "Temp Written Blocks": 0,
                                   "Plans": [
                                     {
                                       "Node Type": "Seq Scan",
                                       "Parent Relationship": "Outer",
                                       "Parallel Aware": false,
                                       "Relation Name": "table_2",
                                       "Alias": "p",
                                       "Startup Cost": 0.00,
                                       "Total Cost": 714.74,
                                       "Plan Rows": 20574,
                                       "Plan Width": 32,
                                       "Actual Startup Time": 0.010,
                                       "Actual Total Time": 5.437,
                                       "Actual Rows": 20698,
                                       "Actual Loops": 1,
                                       "Shared Hit Blocks": 509,
                                       "Shared Read Blocks": 0,
                                       "Shared Dirtied Blocks": 0,
                                       "Shared Written Blocks": 0,
                                       "Local Hit Blocks": 0,
                                       "Local Read Blocks": 0,
                                       "Local Dirtied Blocks": 0,
                                       "Local Written Blocks": 0,
                                       "Temp Read Blocks": 0,
                                       "Temp Written Blocks": 0
                                     }
                                   ]
                                 }
                               ]
                             },
                             {
                               "Node Type": "Index Scan",
                               "Parent Relationship": "Inner",
                               "Parallel Aware": false,
                               "Scan Direction": "Forward",
                               "Index Name": "fs_s_id",
                               "Relation Name": "fs",
                               "Alias": "e",
                               "Startup Cost": 0.43,
                               "Total Cost": 1.24,
                               "Plan Rows": 1,
                               "Plan Width": 28,
                               "Actual Startup Time": 0.005,
                               "Actual Total Time": 0.006,
                               "Actual Rows": 1,
                               "Actual Loops": 204,
                               "Index Cond": "(s_id = p.s_id)",
                               "Rows Removed by Index Recheck": 0,
                               "Filter": "\"b\"",
                               "Rows Removed by Filter": 1,
                               "Shared Hit Blocks": 912,
                               "Shared Read Blocks": 0,
                               "Shared Dirtied Blocks": 0,
                               "Shared Written Blocks": 0,
                               "Local Hit Blocks": 0,
                               "Local Read Blocks": 0,
                               "Local Dirtied Blocks": 0,
                               "Local Written Blocks": 0,
                               "Temp Read Blocks": 0,
                               "Temp Written Blocks": 0
                             }
                           ]
                         },
                         {
                           "Node Type": "Index Scan",
                           "Parent Relationship": "Inner",
                           "Parallel Aware": false,
                           "Scan Direction": "Forward",
                           "Index Name": "table_3_pkey",
                           "Relation Name": "table_3",
                           "Alias": "u",
                           "Startup Cost": 0.29,
                           "Total Cost": 0.35,
                           "Plan Rows": 1,
                           "Plan Width": 14,
                           "Actual Startup Time": 0.004,
                           "Actual Total Time": 0.004,
                           "Actual Rows": 1,
                           "Actual Loops": 208,
                           "Index Cond": "(id = p.s_id)",
                           "Rows Removed by Index Recheck": 0,
                           "Shared Hit Blocks": 626,
                           "Shared Read Blocks": 0,
                           "Shared Dirtied Blocks": 0,
                           "Shared Written Blocks": 0,
                           "Local Hit Blocks": 0,
                           "Local Read Blocks": 0,
                           "Local Dirtied Blocks": 0,
                           "Local Written Blocks": 0,
                           "Temp Read Blocks": 0,
                           "Temp Written Blocks": 0
                         }
                       ]
                     },
                     {
                       "Node Type": "Index Scan",
                       "Parent Relationship": "Inner",
                       "Parallel Aware": false,
                       "Scan Direction": "Forward",
                       "Index Name": "s_id_unique_idx",
                       "Relation Name": "table_4",
                       "Alias": "j",
                       "Startup Cost": 0.28,
                       "Total Cost": 0.30,
                       "Plan Rows": 1,
                       "Plan Width": 75,
                       "Actual Startup Time": 0.002,
                       "Actual Total Time": 0.002,
                       "Actual Rows": 0,
                       "Actual Loops": 208,
                       "Index Cond": "(u.id = s_id)",
                       "Rows Removed by Index Recheck": 0,
                       "Shared Hit Blocks": 488,
                       "Shared Read Blocks": 0,
                       "Shared Dirtied Blocks": 0,
                       "Shared Written Blocks": 0,
                       "Local Hit Blocks": 0,
                       "Local Read Blocks": 0,
                       "Local Dirtied Blocks": 0,
                       "Local Written Blocks": 0,
                       "Temp Read Blocks": 0,
                       "Temp Written Blocks": 0
                     }
                   ]
                 },
                 {
                   "Node Type": "Index Scan",
                   "Parent Relationship": "Inner",
                   "Parallel Aware": false,
                   "Scan Direction": "Forward",
                   "Index Name": "table_7_pkey",
                   "Relation Name": "table_7",
                   "Alias": "d",
                   "Startup Cost": 0.29,
                   "Total Cost": 0.33,
                   "Plan Rows": 1,
                   "Plan Width": 8,
                   "Actual Startup Time": 0.004,
                   "Actual Total Time": 0.004,
                   "Actual Rows": 1,
                   "Actual Loops": 208,
                   "Index Cond": "(u.id = s_id)",
                   "Rows Removed by Index Recheck": 0,
                   "Shared Hit Blocks": 624,
                   "Shared Read Blocks": 0,
                   "Shared Dirtied Blocks": 0,
                   "Shared Written Blocks": 0,
                   "Local Hit Blocks": 0,
                   "Local Read Blocks": 0,
                   "Local Dirtied Blocks": 0,
                   "Local Written Blocks": 0,
                   "Temp Read Blocks": 0,
                   "Temp Written Blocks": 0
                 }
               ]
             }
           ]
         }
       ]
     },
     "Planning Time": 1.690,
     "Triggers": [
     ],
     "Execution Time": 3565.594
   }
 ]
`

const EXPECTED_TEXT_1 = ` Unique  (cost=156506.25..156507.37 rows=225 width=149) (actual time=3684.547..3684.613 rows=204 loops=1)
   Buffers: shared hit=43757
   CTE table_5
     ->  Unique  (cost=111147.74..111155.68 rows=1588 width=4) (actual time=3649.950..3653.392 rows=204 loops=1)
           Buffers: shared hit=37235
           ->  Sort  (cost=111147.74..111151.71 rows=1588 width=4) (actual time=3649.949..3651.878 rows=17434 loops=1)
                 Sort Key: u_1.id
                 Sort Method: quicksort  Memory: 1586kB
                 Buffers: shared hit=37235
                 ->  Hash Left Join  (cost=2465.41..111063.32 rows=1588 width=4) (actual time=55.487..3645.938 rows=17434 loops=1)
                       Hash Cond: (u_1.id = j_1.s_id)
                       Filter: ((u_1.created_at >= (now() - '3 days'::interval)) OR (u_1.updated_at >= now()) OR (p_1.created_at >= (now() - '3 days'::interval)) OR (p_1.updated_at >= now()) OR (c_1.created_at >= (now() - '3 days'::interval)) OR (c_1.updated_at >= now()) OR (j_1.created_at >= (now() - '3 days'::interval)))
                       Rows Removed by Filter: 2264748
                       Buffers: shared hit=37235
                       ->  Hash Join  (cost=2402.70..92272.71 rows=2271649 width=52) (actual time=28.106..1195.591 rows=2282182 loops=1)
                             Hash Cond: (c_1.s_id = u_1.id)
                             Buffers: shared hit=37210
                             ->  Seq Scan on table_1 c_1  (cost=0.00..58660.93 rows=2264693 width=20) (actual time=0.009..204.214 rows=2281919 loops=1)
                                   Buffers: shared hit=36014
                             ->  Hash  (cost=2145.52..2145.52 rows=20574 width=40) (actual time=28.029..28.029 rows=20698 loops=1)
                                   Buckets: 32768  Batches: 1  Memory Usage: 1874kB
                                   Buffers: shared hit=1196
                                   ->  Hash Join  (cost=1148.50..2145.52 rows=20574 width=40) (actual time=8.890..23.319 rows=20698 loops=1)
                                         Hash Cond: (p_1.s_id = u_1.id)
                                         Buffers: shared hit=1196
                                         ->  Seq Scan on table_2 p_1  (cost=0.00..714.74 rows=20574 width=20) (actual time=0.005..6.016 rows=20698 loops=1)
                                               Buffers: shared hit=509
                                         ->  Hash  (cost=892.11..892.11 rows=20511 width=20) (actual time=8.802..8.802 rows=20697 loops=1)
                                               Buckets: 32768  Batches: 1  Memory Usage: 1388kB
                                               Buffers: shared hit=687
                                               ->  Seq Scan on table_3 u_1  (cost=0.00..892.11 rows=20511 width=20) (actual time=0.007..5.432 rows=20697 loops=1)
                                                     Buffers: shared hit=687
                       ->  Hash  (cost=41.76..41.76 rows=1676 width=12) (actual time=0.566..0.566 rows=1640 loops=1)
                             Buckets: 2048  Batches: 1  Memory Usage: 93kB
                             Buffers: shared hit=25
                             ->  Seq Scan on table_4 j_1  (cost=0.00..41.76 rows=1676 width=12) (actual time=0.014..0.306 rows=1640 loops=1)
                                   Buffers: shared hit=25
   CTE table_6
     ->  HashAggregate  (cost=43980.17..43982.17 rows=200 width=12) (actual time=3670.930..3671.011 rows=204 loops=1)
           Group Key: u_2.id
           Buffers: shared hit=40598
           ->  Nested Loop  (cost=0.43..42378.81 rows=320272 width=8) (actual time=3649.978..3666.324 rows=22180 loops=1)
                 Buffers: shared hit=40598
                 ->  CTE Scan on table_5 u_2  (cost=0.00..31.76 rows=1588 width=4) (actual time=3649.952..3653.435 rows=204 loops=1)
                       Buffers: shared hit=37235
                 ->  Index Scan using table_1_s_id on table_1 c_2  (cost=0.43..24.65 rows=202 width=8) (actual time=0.008..0.038 rows=109 loops=204)
                       Index Cond: (s_id = u_2.id)
                       Buffers: shared hit=3363
   ->  Sort  (cost=1368.40..1368.96 rows=225 width=149) (actual time=3684.546..3684.579 rows=208 loops=1)
         Sort Key: u.id
         Sort Method: quicksort  Memory: 63kB
         Buffers: shared hit=43757
         ->  Nested Loop Left Join  (cost=973.20..1359.61 rows=225 width=149) (actual time=3680.276..3684.426 rows=208 loops=1)
               Buffers: shared hit=43757
               ->  Nested Loop Left Join  (cost=972.91..1291.89 rows=197 width=145) (actual time=3680.263..3683.407 rows=208 loops=1)
                     Buffers: shared hit=43133
                     ->  Nested Loop  (cost=972.63..1231.49 rows=197 width=74) (actual time=3680.251..3682.971 rows=208 loops=1)
                           Buffers: shared hit=42645
                           ->  Nested Loop  (cost=972.34..1228.65 rows=8 width=72) (actual time=3680.236..3682.113 rows=208 loops=1)
                                 Buffers: shared hit=42019
                                 ->  Hash Join  (cost=971.91..978.91 rows=200 width=44) (actual time=3680.214..3680.529 rows=204 loops=1)
                                       Hash Cond: (c.s_id = p.s_id)
                                       Buffers: shared hit=41107
                                       ->  CTE Scan on table_6 c  (cost=0.00..4.00 rows=200 width=12) (actual time=3670.933..3671.064 rows=204 loops=1)
                                             Buffers: shared hit=40598
                                       ->  Hash  (cost=714.74..714.74 rows=20574 width=32) (actual time=9.210..9.210 rows=20698 loops=1)
                                             Buckets: 32768  Batches: 1  Memory Usage: 1473kB
                                             Buffers: shared hit=509
                                             ->  Seq Scan on table_2 p  (cost=0.00..714.74 rows=20574 width=32) (actual time=0.010..5.437 rows=20698 loops=1)
                                                   Buffers: shared hit=509
                                 ->  Index Scan using fs_s_id on fs e  (cost=0.43..1.24 rows=1 width=28) (actual time=0.005..0.006 rows=1 loops=204)
                                       Index Cond: (s_id = p.s_id)
                                       Filter: "b"
                                       Rows Removed by Filter: 1
                                       Buffers: shared hit=912
                           ->  Index Scan using table_3_pkey on table_3 u  (cost=0.29..0.35 rows=1 width=14) (actual time=0.004..0.004 rows=1 loops=208)
                                 Index Cond: (id = p.s_id)
                                 Buffers: shared hit=626
                     ->  Index Scan using s_id_unique_idx on table_4 j  (cost=0.28..0.30 rows=1 width=75) (actual time=0.002..0.002 rows=0 loops=208)
                           Index Cond: (u.id = s_id)
                           Buffers: shared hit=488
               ->  Index Scan using table_7_pkey on table_7 d  (cost=0.29..0.33 rows=1 width=8) (actual time=0.004..0.004 rows=1 loops=208)
                     Index Cond: (u.id = s_id)
                     Buffers: shared hit=624
`

const INPUT_JSON_2 = `[
  {
    "Plan": {
      "Node Type": "Limit",
      "Parallel Aware": false,
      "Startup Cost": 0.43,
      "Total Cost": 8.45,
      "Plan Rows": 1,
      "Plan Width": 22,
      "Actual Startup Time": 0.026,
      "Actual Total Time": 0.035,
      "Actual Rows": 1,
      "Actual Loops": 1,
      "Shared Hit Blocks": 4,
      "Shared Read Blocks": 0,
      "Shared Dirtied Blocks": 0,
      "Shared Written Blocks": 0,
      "Local Hit Blocks": 0,
      "Local Read Blocks": 0,
      "Local Dirtied Blocks": 0,
      "Local Written Blocks": 0,
      "Temp Read Blocks": 0,
      "Temp Written Blocks": 0,
      "Plans": [
        {
          "Node Type": "Index Only Scan",
          "Parent Relationship": "Outer",
          "Parallel Aware": false,
          "Scan Direction": "Forward",
          "Index Name": "i_user_col",
          "Relation Name": "table_1",
          "Alias": "table_1",
          "Startup Cost": 0.43,
          "Total Cost": 8.45,
          "Plan Rows": 1,
          "Plan Width": 22,
          "Actual Startup Time": 0.021,
          "Actual Total Time": 0.026,
          "Actual Rows": 1,
          "Actual Loops": 1,
          "Index Cond": "(col = 'xxxx'::text)",
          "Rows Removed by Index Recheck": 0,
          "Heap Fetches": 0,
          "Shared Hit Blocks": 4,
          "Shared Read Blocks": 0,
          "Shared Dirtied Blocks": 0,
          "Shared Written Blocks": 0,
          "Local Hit Blocks": 0,
          "Local Read Blocks": 0,
          "Local Dirtied Blocks": 0,
          "Local Written Blocks": 0,
          "Temp Read Blocks": 0,
          "Temp Written Blocks": 0
        }
      ]
    },
    "Planning Time": 0.110,
    "Triggers": [
    ],
    "Execution Time": 0.199
  }
]`

const EXPECTED_TEXT_2 = ` Limit  (cost=0.43..8.45 rows=1 width=22) (actual time=0.026..0.035 rows=1 loops=1)
   Buffers: shared hit=4
   ->  Index Only Scan using i_user_col on table_1  (cost=0.43..8.45 rows=1 width=22) (actual time=0.021..0.026 rows=1 loops=1)
         Index Cond: (col = 'xxxx'::text)
         Heap Fetches: 0
         Buffers: shared hit=4
`

const INPUT_JSON_3 = `[
  {
    "Plan": {
      "Node Type": "Gather",
      "Parallel Aware": false,
      "Startup Cost": 1000.00,
      "Total Cost": 107758.40,
      "Plan Rows": 104,
      "Plan Width": 0,
      "Actual Startup Time": 0.772,
      "Actual Total Time": 1393.167,
      "Actual Rows": 101,
      "Actual Loops": 1,
      "Workers Planned": 2,
      "Workers Launched": 2,
      "Single Copy": false,
      "Shared Hit Blocks": 2528,
      "Shared Read Blocks": 41720,
      "Shared Dirtied Blocks": 0,
      "Shared Written Blocks": 0,
      "Local Hit Blocks": 0,
      "Local Read Blocks": 0,
      "Local Dirtied Blocks": 0,
      "Local Written Blocks": 0,
      "Temp Read Blocks": 0,
      "Temp Written Blocks": 0,
      "Plans": [
        {
          "Node Type": "Seq Scan",
          "Parent Relationship": "Outer",
          "Parallel Aware": true,
          "Relation Name": "bbb",
          "Alias": "bbb",
          "Startup Cost": 0.00,
          "Total Cost": 106748.00,
          "Plan Rows": 43,
          "Plan Width": 0,
          "Actual Startup Time": 918.248,
          "Actual Total Time": 1380.403,
          "Actual Rows": 34,
          "Actual Loops": 3,
          "Filter": "((i >= 100) AND (i <= 200))",
          "Rows Removed by Filter": 3333300,
          "Shared Hit Blocks": 2528,
          "Shared Read Blocks": 41720,
          "Shared Dirtied Blocks": 0,
          "Shared Written Blocks": 0,
          "Local Hit Blocks": 0,
          "Local Read Blocks": 0,
          "Local Dirtied Blocks": 0,
          "Local Written Blocks": 0,
          "Temp Read Blocks": 0,
          "Temp Written Blocks": 0
        }
      ]
    },
    "Planning Time": 0.158,
    "Triggers": [
    ],
    "Execution Time": 1299.614
  }
]`

const EXPECTED_TEXT_3 = ` Gather  (cost=1000.00..107758.40 rows=104 width=0) (actual time=0.772..1393.167 rows=101 loops=1)
   Workers Planned: 2
   Workers Launched: 2
   Buffers: shared hit=2528 read=41720
   ->  Parallel Seq Scan on bbb  (cost=0.00..106748.00 rows=43 width=0) (actual time=918.248..1380.403 rows=34 loops=3)
         Filter: ((i >= 100) AND (i <= 200))
         Rows Removed by Filter: 3333300
         Buffers: shared hit=2528 read=41720
`

const INPUT_JSON_4 = `[
  {
    "Plan": {
      "Node Type": "Limit",
      "Parallel Aware": false,
      "Startup Cost": 110.74,
      "Total Cost": 111.85,
      "Plan Rows": 20,
      "Plan Width": 851,
      "Actual Startup Time": 9.347,
      "Actual Total Time": 9.357,
      "Actual Rows": 20,
      "Actual Loops": 1,
      "Shared Hit Blocks": 8,
      "Shared Read Blocks": 71,
      "Shared Dirtied Blocks": 2,
      "Shared Written Blocks": 2,
      "Local Hit Blocks": 0,
      "Local Read Blocks": 0,
      "Local Dirtied Blocks": 0,
      "Local Written Blocks": 0,
      "Temp Read Blocks": 0,
      "Temp Written Blocks": 0,
      "I/O Read Time": 7.150,
      "I/O Write Time": 0.370,
      "Plans": [
        {
          "Node Type": "Seq Scan",
          "Parent Relationship": "Outer",
          "Parallel Aware": false,
          "Relation Name": "users",
          "Alias": "users",
          "Startup Cost": 0.00,
          "Total Cost": 384481.33,
          "Plan Rows": 6943833,
          "Plan Width": 851,
          "Actual Startup Time": 0.017,
          "Actual Total Time": 9.196,
          "Actual Rows": 2020,
          "Actual Loops": 1,
          "Shared Hit Blocks": 8,
          "Shared Read Blocks": 71,
          "Shared Dirtied Blocks": 2,
          "Shared Written Blocks": 2,
          "Local Hit Blocks": 0,
          "Local Read Blocks": 0,
          "Local Dirtied Blocks": 0,
          "Local Written Blocks": 0,
          "Temp Written Blocks": 0,
          "I/O Read Time": 7.150,
          "I/O Write Time": 0.370
        }
      ]
    },
    "Planning Time": 0.173,
    "Triggers": [
    ],
    "Execution Time": 9.407
  }
]`

const EXPECTED_TEXT_4 = ` Limit  (cost=110.74..111.85 rows=20 width=851) (actual time=9.347..9.357 rows=20 loops=1)
   Buffers: shared hit=8 read=71 dirtied=2 written=2
   I/O Timings: read=7.150 write=0.370
   ->  Seq Scan on users  (cost=0.00..384481.33 rows=6943833 width=851) (actual time=0.017..9.196 rows=2020 loops=1)
         Buffers: shared hit=8 read=71 dirtied=2 written=2
         I/O Timings: read=7.150 write=0.370
`

/*
2019 © Postgres.ai
*/

package command

const PlanSize = 400

const MsgExecOptionReq = "Use `exec` to run query, e.g. `exec drop index some_index_name`"
const MsgExplainOptionReq = "Use `explain` to see the query's plan, e.g. `explain select 1`"
const MsgSnapshotOptionReq = "Use `snapshot` to create a snapshot, e.g. `snapshot state_name`"

const SeparatorPlan = "\n[...SKIP...]\n"

const CutText = "_(The text in the preview above has been cut)_"

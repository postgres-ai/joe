/*
2019 © Postgres.ai
Based on the code from Simon Engledew @ https://github.com/simon-engledew/gocmdpev
*/

// Package pgexplain provides tools for Postgres explain processing.
package pgexplain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"gitlab.com/postgres-ai/joe/pkg/util"
)

type EstimateDirection string

const (
	Over  EstimateDirection = "Over"
	Under                   = "Under"
)

type NodeType string

// Node types as they appear in EXPLAIN's "Node Type" field. The values must match
// PostgreSQL verbatim (e.g. "ModifyTable" has no space, "Seq Scan" does). Types
// without a special case fall through to the generic renderer, which prints the
// raw node-type string.
const (
	Limit           NodeType = "Limit"
	Append          NodeType = "Append"
	MergeAppend     NodeType = "Merge Append"
	RecursiveUnion  NodeType = "Recursive Union"
	Sort            NodeType = "Sort"
	IncrementalSort NodeType = "Incremental Sort"
	NestedLoop      NodeType = "Nested Loop"
	MergeJoin       NodeType = "Merge Join"
	Hash            NodeType = "Hash"
	HashJoin        NodeType = "Hash Join"
	Aggregate       NodeType = "Aggregate"
	GroupAggregate  NodeType = "Group"
	WindowAgg       NodeType = "WindowAgg"
	Unique          NodeType = "Unique"
	SetOp           NodeType = "SetOp"
	Gather          NodeType = "Gather"
	GatherMerge     NodeType = "Gather Merge"
	Memoize         NodeType = "Memoize"
	Materialize     NodeType = "Materialize"
	Result          NodeType = "Result"
	ProjectSet      NodeType = "ProjectSet"
	LockRows        NodeType = "LockRows"
	BitmapAnd       NodeType = "BitmapAnd"
	BitmapOr        NodeType = "BitmapOr"
	SequenceScan    NodeType = "Seq Scan"
	SampleScan      NodeType = "Sample Scan"
	IndexScan       NodeType = "Index Scan"
	IndexOnlyScan   NodeType = "Index Only Scan"
	BitmapHeapScan  NodeType = "Bitmap Heap Scan"
	BitmapIndexScan NodeType = "Bitmap Index Scan"
	TidScan         NodeType = "Tid Scan"
	TidRangeScan    NodeType = "Tid Range Scan"
	CTEScan         NodeType = "CTE Scan"
	NamedTuplestore NodeType = "Named Tuplestore Scan"
	WorkTableScan   NodeType = "WorkTable Scan"
	FunctionScan    NodeType = "Function Scan"
	TableFuncScan   NodeType = "Table Function Scan"
	SubqueryScan    NodeType = "Subquery Scan"
	ValuesScan      NodeType = "Values Scan"
	ForeignScan     NodeType = "Foreign Scan"
	CustomScan      NodeType = "Custom Scan"
	ModifyTable     NodeType = "ModifyTable"
)

type Explain struct {
	Plan     Plan      `json:"Plan"`
	Triggers []Trigger `json:"Triggers"`

	QueryIdentifier uint64            `json:"Query Identifier"`
	Settings        map[string]string `json:"Settings"`
	PlanningTime    float64           `json:"Planning Time"`
	ExecutionTime   float64           `json:"Execution Time"`
	TotalTime       float64

	TotalCost float64

	// Buffers.
	SharedHitBlocks     uint64
	SharedDirtiedBlocks uint64
	SharedReadBlocks    uint64
	SharedWrittenBlocks uint64
	LocalHitBlocks      uint64
	LocalReadBlocks     uint64
	LocalDirtiedBlocks  uint64
	LocalWrittenBlocks  uint64
	TempReadBlocks      uint64
	TempWrittenBlocks   uint64

	// IO timing.
	IOReadTime  *float64
	IOWriteTime *float64

	ActualRows      float64
	MaxRows         float64
	MaxCost         float64
	MaxDuration     float64
	ContainsSeqScan bool
}

// Trigger describes triggers in the explain output.
type Trigger struct {
	Name           string  `json:"Trigger Name"`
	ConstraintName string  `json:"Constraint Name"`
	Relation       string  `json:"Relation"`
	Time           float64 `json:"Time"`
	Calls          uint64  `json:"Calls"`
}

type Plan struct {
	Plans []Plan `json:"Plans"`

	// Buffers.
	SharedHitBlocks     uint64 `json:"Shared Hit Blocks"`
	SharedReadBlocks    uint64 `json:"Shared Read Blocks"`
	SharedDirtiedBlocks uint64 `json:"Shared Dirtied Blocks"`
	SharedWrittenBlocks uint64 `json:"Shared Written Blocks"`
	LocalHitBlocks      uint64 `json:"Local Hit Blocks"`
	LocalReadBlocks     uint64 `json:"Local Read Blocks"`
	LocalDirtiedBlocks  uint64 `json:"Local Dirtied Blocks"`
	LocalWrittenBlocks  uint64 `json:"Local Written Blocks"`
	TempReadBlocks      uint64 `json:"Temp Read Blocks"`
	TempWrittenBlocks   uint64 `json:"Temp Written Blocks"`

	// IO timing. PostgreSQL 17+ replaces the aggregate fields with per-buffer-type splits;
	// normalizeIOTiming folds the splits back into IOReadTime/IOWriteTime when those are nil.
	IOReadTime        *float64 `json:"I/O Read Time,omitempty"`         // ms
	IOWriteTime       *float64 `json:"I/O Write Time,omitempty"`        // ms
	SharedIOReadTime  *float64 `json:"Shared I/O Read Time,omitempty"`  // ms
	SharedIOWriteTime *float64 `json:"Shared I/O Write Time,omitempty"` // ms
	LocalIOReadTime   *float64 `json:"Local I/O Read Time,omitempty"`   // ms
	LocalIOWriteTime  *float64 `json:"Local I/O Write Time,omitempty"`  // ms
	TempIOReadTime    *float64 `json:"Temp I/O Read Time,omitempty"`    // ms
	TempIOWriteTime   *float64 `json:"Temp I/O Write Time,omitempty"`   // ms

	// Actual.
	ActualLoops       uint64  `json:"Actual Loops"`
	ActualRows        float64 `json:"Actual Rows"` // PostgreSQL 18+ reports this as a fraction (rows averaged over loops).
	ActualStartupTime float64 `json:"Actual Startup Time"`
	ActualTotalTime   float64 `json:"Actual Total Time"`

	// Estimates.
	PlanRows    uint64  `json:"Plan Rows"`
	PlanWidth   uint64  `json:"Plan Width"`
	StartupCost float64 `json:"Startup Cost"`
	TotalCost   float64 `json:"Total Cost"`

	// WAL.
	WALRecords     uint64 `json:"WAL Records,omitempty"`
	WALFPI         uint64 `json:"WAL FPI,omitempty"`
	WALBytes       uint64 `json:"WAL Bytes,omitempty"`
	WALBuffersFull uint64 `json:"WAL Buffers Full,omitempty"` // PostgreSQL 18+

	// PostgreSQL 18+ per-node fields. All are absent on older servers, so the
	// renderers below only emit them when present (true / non-zero / non-empty),
	// which keeps pre-18 output unchanged.
	Disabled       bool   `json:"Disabled,omitempty"`        // cost-based node disablement (was disable_cost)
	IndexSearches  uint64 `json:"Index Searches,omitempty"`  // Index/Index-Only/Bitmap-Index Scan
	Storage        string `json:"Storage,omitempty"`         // Material/WindowAgg/CTE, e.g. "Memory"
	MaximumStorage uint64 `json:"Maximum Storage,omitempty"` // kB

	// General.
	Alias                     string   `json:"Alias"`
	CteName                   string   `json:"CTE Name"`
	Filter                    string   `json:"Filter"`
	FunctionName              string   `json:"Function Name"`
	GroupKey                  []string `json:"Group Key"`
	HashBatches               uint64   `json:"Hash Batches"`
	HashBuckets               uint64   `json:"Hash Buckets"`
	HashCondition             string   `json:"Hash Cond"`
	HeapFetches               uint64   `json:"Heap Fetches"`
	IndexCondition            string   `json:"Index Cond"`
	IndexName                 string   `json:"Index Name"`
	MergeCondition            string   `json:"Merge Cond"`
	JoinType                  string   `json:"Join Type"`
	NodeType                  NodeType `json:"Node Type"`
	Operation                 string   `json:"Operation"`
	OriginalHashBatches       uint64   `json:"Original Hash Batches"`
	OriginalHashBuckets       uint64   `json:"Original Hash Buckets"`
	Output                    []string `json:"Output"`
	ParallelAware             bool     `json:"Parallel Aware"`
	ParentRelationship        string   `json:"Parent Relationship"`
	PeakMemoryUsage           uint64   `json:"Peak Memory Usage"` // kB
	RelationName              string   `json:"Relation Name"`
	RowsRemovedByFilter       uint64   `json:"Rows Removed by Filter"`
	RowsRemovedByIndexRecheck uint64   `json:"Rows Removed by Index Recheck"`
	ScanDirection             string   `json:"Scan Direction"`
	Schema                    string   `json:"Schema"`
	SortKey                   []string `json:"Sort Key"`
	SortMethod                string   `json:"Sort Method"`
	SortSpaceType             string   `json:"Sort Space Type"`
	SortSpaceUsed             uint64   `json:"Sort Space Used"` // kB
	Strategy                  string   `json:"Strategy"`
	SubplanName               string   `json:"Subplan Name"`
	WorkersLaunched           uint     `json:"Workers Launched"`
	WorkersPlanned            uint     `json:"Workers Planned"`

	// Calculated params.
	ActualCost                  float64
	ActualDuration              float64
	Costliest                   bool
	Largest                     bool
	PlannerRowEstimateDirection EstimateDirection
	PlannerRowEstimateFactor    float64
	Slowest                     bool
}

type Tip struct {
	Code        string `yaml:"code"`
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	DetailsUrl  string `yaml:"detailsUrl"`
}

// Explain Processing.
func NewExplain(explainJSON string) (*Explain, error) {
	var explains []Explain

	err := json.NewDecoder(strings.NewReader(explainJSON)).Decode(&explains)

	if err != nil {
		return nil, err
	}

	if len(explains) == 0 {
		return nil, errors.New("Empty explain")
	}

	// TODO(anatoly): Is it possible to have more than one explain?
	var ex = &explains[0]
	ex.processExplain()

	return ex, nil
}

func (ex *Explain) RenderPlanText() string {
	buf := new(bytes.Buffer)
	ex.writeExplainText(buf)
	return buf.String()
}

func (ex *Explain) RenderStats() string {
	buf := new(bytes.Buffer)
	ex.writeStatsText(buf)
	return buf.String()
}

func (ex *Explain) processExplain() {
	ex.Plan.normalizeIOTiming()
	ex.calculateParams()

	ex.processPlan(&ex.Plan)
	ex.calculateOutlierNodes(&ex.Plan)
}

// normalizeIOTiming folds PostgreSQL 17+ per-buffer-type I/O timings
// (Shared/Local/Temp) into the legacy IOReadTime/IOWriteTime fields when those
// are absent, so downstream rendering stays version-agnostic.
func (plan *Plan) normalizeIOTiming() {
	if plan.IOReadTime == nil {
		plan.IOReadTime = sumFloat64Pointers(plan.SharedIOReadTime, plan.LocalIOReadTime, plan.TempIOReadTime)
	}

	if plan.IOWriteTime == nil {
		plan.IOWriteTime = sumFloat64Pointers(plan.SharedIOWriteTime, plan.LocalIOWriteTime, plan.TempIOWriteTime)
	}

	for index := range plan.Plans {
		plan.Plans[index].normalizeIOTiming()
	}
}

func sumFloat64Pointers(values ...*float64) *float64 {
	var (
		total float64
		found bool
	)

	for _, value := range values {
		if value == nil {
			continue
		}

		total += *value
		found = true
	}

	if !found {
		return nil
	}

	return &total
}

func (ex *Explain) calculateParams() {
	ex.ActualRows = ex.Plan.ActualRows

	ex.SharedHitBlocks = ex.Plan.SharedHitBlocks
	ex.SharedReadBlocks = ex.Plan.SharedReadBlocks
	ex.SharedDirtiedBlocks = ex.Plan.SharedDirtiedBlocks
	ex.SharedWrittenBlocks = ex.Plan.SharedWrittenBlocks

	ex.LocalHitBlocks = ex.Plan.LocalHitBlocks
	ex.LocalReadBlocks = ex.Plan.LocalReadBlocks
	ex.LocalDirtiedBlocks = ex.Plan.LocalDirtiedBlocks
	ex.LocalWrittenBlocks = ex.Plan.LocalWrittenBlocks

	ex.TempReadBlocks = ex.Plan.TempReadBlocks
	ex.TempWrittenBlocks = ex.Plan.TempWrittenBlocks

	ex.TotalTime = ex.PlanningTime + ex.ExecutionTime
	ex.IOReadTime = ex.Plan.IOReadTime
	ex.IOWriteTime = ex.Plan.IOWriteTime
}

func (ex *Explain) processPlan(plan *Plan) {
	ex.checkSeqScan(plan)
	ex.calculatePlannerEstimate(plan)
	ex.calculateActuals(plan)
	ex.calculateMaximums(plan)

	for index := range plan.Plans {
		ex.processPlan(&plan.Plans[index])
	}
}

func (ex *Explain) checkSeqScan(plan *Plan) {
	ex.ContainsSeqScan = ex.ContainsSeqScan || plan.NodeType == SequenceScan
}

func (ex *Explain) calculatePlannerEstimate(plan *Plan) {
	plan.PlannerRowEstimateFactor = 0

	if float64(plan.PlanRows) == plan.ActualRows {
		return
	}

	plan.PlannerRowEstimateDirection = Under
	if plan.PlanRows != 0 {
		plan.PlannerRowEstimateFactor = plan.ActualRows / float64(plan.PlanRows)
	}

	if plan.PlannerRowEstimateFactor < 1.0 {
		plan.PlannerRowEstimateFactor = 0
		plan.PlannerRowEstimateDirection = Over
		if plan.ActualRows != 0 {
			plan.PlannerRowEstimateFactor = float64(plan.PlanRows) / plan.ActualRows
		}
	}
}

func (ex *Explain) calculateActuals(plan *Plan) {
	plan.ActualDuration = plan.ActualTotalTime
	plan.ActualCost = plan.TotalCost

	for _, child := range plan.Plans {
		if child.NodeType != CTEScan {
			plan.ActualDuration = plan.ActualDuration - child.ActualTotalTime
			plan.ActualCost = plan.ActualCost - child.TotalCost
		}
	}

	if plan.ActualCost < 0 {
		plan.ActualCost = 0
	}

	ex.TotalCost = ex.TotalCost + plan.ActualCost

	plan.ActualDuration = plan.ActualDuration * float64(plan.ActualLoops)
}

func (ex *Explain) calculateMaximums(plan *Plan) {
	if ex.MaxRows < plan.ActualRows {
		ex.MaxRows = plan.ActualRows
	}
	if ex.MaxCost < plan.ActualCost {
		ex.MaxCost = plan.ActualCost
	}
	if ex.MaxDuration < plan.ActualDuration {
		ex.MaxDuration = plan.ActualDuration
	}
}

func (ex *Explain) calculateOutlierNodes(plan *Plan) {
	plan.Costliest = plan.ActualCost == ex.MaxCost
	plan.Largest = plan.ActualRows == ex.MaxRows
	plan.Slowest = plan.ActualDuration == ex.MaxDuration

	for index := range plan.Plans {
		ex.calculateOutlierNodes(&plan.Plans[index])
	}
}

func (ex *Explain) writeExplainText(writer io.Writer) {
	ex.writePlanText(writer, &ex.Plan, " ", 0, true)

	if len(ex.Triggers) > 0 {
		_, _ = fmt.Fprint(writer, printTriggers(ex.Triggers))
	}

	if len(ex.Settings) > 0 {
		_, _ = fmt.Fprintf(writer, "Settings: %s\n", printMap(ex.Settings))
	}

	if ex.QueryIdentifier != 0 {
		_, _ = fmt.Fprintf(writer, "Query ID: %d\n", ex.QueryIdentifier)
	}
}

func (ex *Explain) writeExplainTextWithoutCosts(writer io.Writer) {
	ex.writePlanText(writer, &ex.Plan, " ", 0, false)

	if len(ex.Triggers) > 0 {
		_, _ = fmt.Fprint(writer, printTriggers(ex.Triggers))
	}

	if len(ex.Settings) > 0 {
		_, _ = fmt.Fprintf(writer, "Settings: %s\n", printMap(ex.Settings))
	}
}

// nolint
func (ex *Explain) writeStatsText(writer io.Writer) {
	fmt.Fprintf(writer, "\nTime: %s\n", util.MillisecondsToString(ex.TotalTime))
	fmt.Fprintf(writer, "  - planning: %s\n", util.MillisecondsToString(ex.PlanningTime))
	fmt.Fprintf(writer, "  - execution: %s\n", util.MillisecondsToString(ex.ExecutionTime))

	ioRead := util.NA
	if ex.IOReadTime != nil {
		ioRead = util.MillisecondsToString(*ex.IOReadTime)
	}

	fmt.Fprintf(writer, "    - I/O read: %s\n", ioRead)

	ioWrite := util.NA
	if ex.IOWriteTime != nil {
		ioWrite = util.MillisecondsToString(*ex.IOWriteTime)
	}

	fmt.Fprintf(writer, "    - I/O write: %s\n", ioWrite)

	fmt.Fprintf(writer, "\nShared buffers:\n")
	ex.writeBlocks(writer, "hits", ex.SharedHitBlocks, "from the buffer pool")
	ex.writeBlocks(writer, "reads", ex.SharedReadBlocks, "from the OS file cache, including disk I/O")
	ex.writeBlocks(writer, "dirtied", ex.SharedDirtiedBlocks, "")
	ex.writeBlocks(writer, "writes", ex.SharedWrittenBlocks, "")

	if ex.LocalHitBlocks > 0 || ex.LocalReadBlocks > 0 ||
		ex.LocalDirtiedBlocks > 0 || ex.LocalWrittenBlocks > 0 {
		fmt.Fprintf(writer, "\nLocal buffers:\n")
	}
	if ex.LocalHitBlocks > 0 {
		ex.writeBlocks(writer, "hits", ex.LocalHitBlocks, "")
	}
	if ex.LocalReadBlocks > 0 {
		ex.writeBlocks(writer, "reads", ex.LocalReadBlocks, "")
	}
	if ex.LocalDirtiedBlocks > 0 {
		ex.writeBlocks(writer, "dirtied", ex.LocalDirtiedBlocks, "")
	}
	if ex.LocalWrittenBlocks > 0 {
		ex.writeBlocks(writer, "writes", ex.LocalWrittenBlocks, "")
	}

	if ex.TempReadBlocks > 0 || ex.TempWrittenBlocks > 0 {
		fmt.Fprintf(writer, "\nTemp buffers:\n")
	}
	if ex.TempReadBlocks > 0 {
		ex.writeBlocks(writer, "reads", ex.TempReadBlocks, "")
	}
	if ex.TempWrittenBlocks > 0 {
		ex.writeBlocks(writer, "writes", ex.TempWrittenBlocks, "")
	}
}

func (ex *Explain) writeBlocks(writer io.Writer, name string, blocks uint64, cmmt string) {
	if len(cmmt) > 0 {
		cmmt = " " + cmmt
	}

	if blocks == 0 {
		fmt.Fprintf(writer, "  - %s: 0%s\n", name, cmmt)
		return
	}

	fmt.Fprintf(writer, "  - %s: %d (~%s)%s\n", name, blocks, blocksToBytes(blocks), cmmt)
}

func (ex *Explain) writePlanText(writer io.Writer, plan *Plan, prefix string, depth int, withCosts bool) {
	currentPrefix := prefix
	subplanPrefix := ""

	var outputFn = func(format string, a ...interface{}) (int, error) {
		return fmt.Fprintf(writer, fmt.Sprintf("%s%s\n", currentPrefix, format), a...)
	}

	// Treat subplan as additional details.
	if plan.SubplanName != "" {
		writeSubplanTextNodeCaption(outputFn, plan)
		subplanPrefix = "  "
		depth++
	}

	if depth != 0 {
		currentPrefix = prefix + subplanPrefix + "->  "
	}

	writePlanTextNodeCaption(outputFn, plan, withCosts)

	currentPrefix = prefix + "  "
	if depth != 0 {
		currentPrefix = prefix + subplanPrefix + "      "
	}

	writePlanTextNodeDetails(outputFn, plan)

	for index := range plan.Plans {
		ex.writePlanText(writer, &plan.Plans[index], currentPrefix, depth+1, withCosts)
	}
}

func writeSubplanTextNodeCaption(outputFn func(string, ...interface{}) (int, error), plan *Plan) {
	outputFn("%s", plan.SubplanName)
}

func planCostsAndTiming(plan *Plan) string {
	costs := fmt.Sprintf("(cost=%.2f..%.2f rows=%d width=%d)", plan.StartupCost, plan.TotalCost, plan.PlanRows, plan.PlanWidth)
	timing := fmt.Sprintf("(actual time=%.3f..%.3f rows=%s loops=%d)",
		plan.ActualStartupTime, plan.ActualTotalTime, formatActualRows(plan.ActualRows), plan.ActualLoops)

	return fmt.Sprintf("  %s %s", costs, timing)
}

// formatActualRows renders an actual-row count. PostgreSQL 18+ reports fractional
// row counts (averaged over loops), so the value is a float: print it as an integer
// when whole — matching pre-18 output and joe's historical rendering — and with two
// decimals when fractional, matching PostgreSQL 18's text format.
func formatActualRows(rows float64) string {
	if rows == math.Trunc(rows) {
		return strconv.FormatInt(int64(rows), 10)
	}

	return strconv.FormatFloat(rows, 'f', 2, 64)
}

func writePlanTextNodeCaption(outputFn func(string, ...interface{}) (int, error), plan *Plan, withCostsAndTiming bool) {
	costsAndTiming := ""

	if withCostsAndTiming {
		costsAndTiming = planCostsAndTiming(plan)
	}

	using := ""
	if plan.IndexName != "" {
		using = fmt.Sprintf(" using %v", plan.IndexName)
	}

	on := ""
	if plan.RelationName != "" || plan.CteName != "" {
		name := plan.RelationName
		if name == "" {
			name = plan.CteName
		}
		if plan.Schema != "" {
			on = fmt.Sprintf(" on %v.%v", plan.Schema, name)
		} else {
			on = fmt.Sprintf(" on %v", name)
		}
		if plan.Alias != "" && plan.Alias != name {
			on += fmt.Sprintf(" %s", plan.Alias)
		}
	}

	nodeType := string(plan.NodeType)

	switch plan.NodeType {
	case ModifyTable: // E.g. for Insert.
		nodeType = plan.Operation

	case ValuesScan:
		on = fmt.Sprintf(" on %q", plan.Alias)

	case FunctionScan:
		on = fmt.Sprintf(" on %s %s", plan.FunctionName, plan.Alias)

	case SubqueryScan:
		nodeType = fmt.Sprintf("%s on %s", plan.NodeType, plan.Alias)

	case MergeJoin:
		if plan.JoinType != "Inner" {
			nodeType = fmt.Sprintf("Merge %s Join", plan.JoinType)
		}

	case HashJoin:
		if plan.JoinType != "Inner" {
			nodeType = fmt.Sprintf("Hash %s Join", plan.JoinType)
		}
	case Aggregate:
		if plan.Strategy == "Hashed" {
			nodeType = fmt.Sprintf("Hash%v", Aggregate)
		}

	case NestedLoop:
		if plan.JoinType != "Inner" {
			nodeType = fmt.Sprintf("%v %s Join", plan.NodeType, plan.JoinType)
		}
	}

	parallel := ""
	if plan.ParallelAware {
		parallel = "Parallel "
	}

	details := formatDetails(plan)

	_, _ = outputFn("%s%v%s%s%s%s", parallel, nodeType, details, using, on, costsAndTiming)
}

func writePlanTextNodeDetails(outputFn func(string, ...interface{}) (int, error), plan *Plan) {
	// PostgreSQL 18+ marks planner-disabled nodes (enable_* = off) explicitly.
	if plan.Disabled {
		outputFn("Disabled: true")
	}

	if len(plan.SortKey) > 0 {
		keys := ""
		for _, key := range plan.SortKey {
			if keys != "" {
				keys += ", "
			}
			keys += key
		}
		outputFn("Sort Key: %s", keys)
	}

	if plan.SortMethod != "" || plan.SortSpaceType != "" {
		details := ""
		if plan.SortMethod != "" {
			details += fmt.Sprintf("Sort Method: %s", plan.SortMethod)
		}
		if plan.SortSpaceType != "" {
			if details != "" {
				details += "  "
			}
			details += fmt.Sprintf("%s: %dkB", plan.SortSpaceType, plan.SortSpaceUsed)
		}
		outputFn("%s", details)
	}

	if len(plan.GroupKey) > 0 {
		keys := ""
		for _, key := range plan.GroupKey {
			if keys != "" {
				keys += ", "
			}
			keys += key
		}
		outputFn("Group Key: %s", keys)
	}

	if plan.HashBuckets != 0 {
		outputFn("Buckets: %d  Batches: %d  Memory Usage: %dkB", plan.HashBuckets, plan.HashBatches, plan.PeakMemoryUsage)
	}

	if plan.IndexCondition != "" {
		outputFn("Index Cond: %v", plan.IndexCondition)
	}

	if plan.MergeCondition != "" {
		outputFn("Merge Cond: %v", plan.MergeCondition)
	}

	if plan.NodeType == IndexOnlyScan {
		outputFn("Heap Fetches: %d", plan.HeapFetches)
	}

	// PostgreSQL 18+: number of index descents on Index/Index-Only/Bitmap-Index Scans.
	if plan.IndexSearches > 0 {
		outputFn("Index Searches: %d", plan.IndexSearches)
	}

	// PostgreSQL 18+: tuplestore storage on Materialize/WindowAgg/CTE nodes.
	if plan.Storage != "" {
		outputFn("Storage: %s  Maximum Storage: %dkB", plan.Storage, plan.MaximumStorage)
	}

	if plan.HashCondition != "" {
		outputFn("Hash Cond: %v", plan.HashCondition)
	}

	if plan.Filter != "" {
		outputFn("Filter: %v", plan.Filter)
		outputFn("Rows Removed by Filter: %d", plan.RowsRemovedByFilter)
	}

	if plan.WorkersPlanned > 0 {
		outputFn("Workers Planned: %d", plan.WorkersPlanned)
		outputFn("Workers Launched: %d", plan.WorkersLaunched)
	}

	buffers := ""
	if plan.SharedDirtiedBlocks > 0 || plan.SharedHitBlocks > 0 || plan.SharedReadBlocks > 0 || plan.SharedWrittenBlocks > 0 {
		buffers += "shared"
		if plan.SharedHitBlocks > 0 {
			buffers += fmt.Sprintf(" hit=%d", plan.SharedHitBlocks)
		}
		if plan.SharedReadBlocks > 0 {
			buffers += fmt.Sprintf(" read=%d", plan.SharedReadBlocks)
		}
		if plan.SharedDirtiedBlocks > 0 {
			buffers += fmt.Sprintf(" dirtied=%d", plan.SharedDirtiedBlocks)
		}
		if plan.SharedWrittenBlocks > 0 {
			buffers += fmt.Sprintf(" written=%d", plan.SharedWrittenBlocks)
		}
	}
	if plan.LocalDirtiedBlocks > 0 || plan.LocalHitBlocks > 0 || plan.LocalReadBlocks > 0 || plan.LocalWrittenBlocks > 0 {
		if buffers != "" {
			buffers += " "
		}
		buffers += "local"
		if plan.LocalHitBlocks > 0 {
			buffers += fmt.Sprintf(" hit=%d", plan.LocalHitBlocks)
		}
		if plan.LocalReadBlocks > 0 {
			buffers += fmt.Sprintf(" read=%d", plan.LocalReadBlocks)
		}
		if plan.LocalDirtiedBlocks > 0 {
			buffers += fmt.Sprintf(" dirtied=%d", plan.LocalDirtiedBlocks)
		}
		if plan.LocalWrittenBlocks > 0 {
			buffers += fmt.Sprintf(" written=%d", plan.LocalWrittenBlocks)
		}
	}

	if buffers != "" {
		outputFn("Buffers: %s", buffers)
	}

	if plan.WALRecords != 0 || plan.WALFPI != 0 || plan.WALBytes != 0 || plan.WALBuffersFull != 0 {
		walLine := fmt.Sprintf("WAL: records=%d fpi=%d bytes=%d", plan.WALRecords, plan.WALFPI, plan.WALBytes)
		if plan.WALBuffersFull != 0 { // PostgreSQL 18+
			walLine += fmt.Sprintf(" buffers-full=%d", plan.WALBuffersFull)
		}

		_, _ = outputFn(walLine)
	}

	ioTiming := ""
	if plan.IOReadTime != nil {
		ioTiming += fmt.Sprintf(" read=%.3f", *plan.IOReadTime)
	}

	if plan.IOWriteTime != nil {
		ioTiming += fmt.Sprintf(" write=%.3f", *plan.IOWriteTime)
	}

	if len(ioTiming) > 0 {
		outputFn("I/O Timings:%s", ioTiming)
	}
}

func formatDetails(plan *Plan) string {
	var details []string

	if plan.ScanDirection != "" && plan.ScanDirection != "Forward" {
		details = append(details, plan.ScanDirection)
	}

	if len(details) > 0 {
		return fmt.Sprintf(" %v", strings.Join(details, ", "))
	}

	return ""
}

func printMap(items map[string]string) string {
	list := make([]string, 0, len(items))

	for key, value := range items {
		list = append(list, fmt.Sprintf("%s = '%v'", key, value))
	}

	return strings.Join(list, ", ")
}

func printTriggers(triggers []Trigger) string {
	sb := strings.Builder{}

	for _, trigger := range triggers {
		fmt.Fprintf(&sb, "Trigger %s for constraint %s: time=%.3f calls=%d\n",
			trigger.Name, trigger.ConstraintName, trigger.Time, trigger.Calls)
	}

	return sb.String()
}

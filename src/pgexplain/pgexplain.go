/*
2019 © Postgres.ai
Based on the code from Simon Engledew @ https://github.com/simon-engledew/gocmdpev
*/

package pgexplain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"../util"
)

type EstimateDirection string

const (
	Over  EstimateDirection = "Over"
	Under                   = "Under"
)

type NodeType string

const (
	Limit           NodeType = "Limit"
	Append                   = "Append"
	Sort                     = "Sort"
	NestedLoop               = "Nested Loop"
	MergeJoin                = "Merge Join"
	Hash                     = "Hash"
	HashJoin                 = "Hash Join"
	Aggregate                = "Aggregate"
	Hashaggregate            = "Hashaggregate"
	SequenceScan             = "Seq Scan"
	IndexScan                = "Index Scan"
	IndexOnlyScan            = "Index Only Scan"
	BitmapHeapScan           = "Bitmap Heap Scan"
	BitmapIndexScan          = "Bitmap Index Scan"
	CTEScan                  = "CTE Scan"
	ModifyTable              = "Modify Table"
)

type Explain struct {
	Plan     Plan          `json:"Plan"`
	Triggers []interface{} `json:"Triggers"`

	PlanningTime  float64 `json:"Planning Time"`
	ExecutionTime float64 `json:"Execution Time"`
	TotalTime     float64

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
	IOReadTime  float64
	IOWriteTime float64

	ActualRows      uint64
	MaxRows         uint64
	MaxCost         float64
	MaxDuration     float64
	ContainsSeqScan bool

	IndexIneffHighFiltered bool
	VacuumAnalyzeNeeded    bool

	Config ExplainConfig `json:"-"`
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

	// IO timing.
	IOReadTime  float64 `json:"I/O Read Time"`  // ms
	IOWriteTime float64 `json:"I/O Write Time"` // ms

	// Actual.
	ActualLoops       uint64  `json:"Actual Loops"`
	ActualRows        uint64  `json:"Actual Rows"`
	ActualStartupTime float64 `json:"Actual Startup Time"`
	ActualTotalTime   float64 `json:"Actual Total Time"`

	// Estimates.
	PlanRows    uint64  `json:"Plan Rows"`
	PlanWidth   uint64  `json:"Plan Width"`
	StartupCost float64 `json:"Startup Cost"`
	TotalCost   float64 `json:"Total Cost"`

	// General.
	Alias                     string   `json:"Alias"`
	CteName                   string   `json:"CTE Name"`
	Filter                    string   `json:"Filter"`
	GroupKey                  []string `json:"Group Key"`
	HashBatches               uint64   `json:"Hash Batches"`
	HashBuckets               uint64   `json:"Hash Buckets"`
	HashCondition             string   `json:"Hash Cond"`
	HeapFetches               uint64   `json:"Heap Fetches"`
	IndexCondition            string   `json:"Index Cond"`
	IndexName                 string   `json:"Index Name"`
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

const (
	TIP_SEQSCAN_USED                    = "SEQSCAN_USED"
	TIP_TOO_MUCH_DATA                   = "TOO_MUCH_DATA"
	TIP_ADD_LIMIT                       = "ADD_LIMIT"
	TIP_TEMP_BUF_WRITTEN                = "TEMP_BUF_WRITTEN"
	TIP_INDEX_INEFFICIENT_HIGH_FILTERED = "INDEX_INEFFICIENT_HIGH_FILTERED"
	TIP_VACUUM_ANALYZE_NEEDED           = "VACUUM_ANALYZE_NEEDED"
)

type ExplainConfig struct {
	Tips   []Tip        `yaml:"tips"`
	Params ParamsConfig `yaml:"params"`
}

type Tip struct {
	Code        string `yaml:"code"`
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	DetailsUrl  string `yaml:"detailsUrl"`
}

// TODO(anatoly): Refactor names.
type ParamsConfig struct {
	// T1 SEQSCAN_USED.
	BuffersHitReadSeqScan uint64 `yaml:"buffersHitReadSeqScan"`

	// T2 TOO_MUCH_DATA.
	BuffersReadBigMax uint64 `yaml:"buffersReadBigMax"`
	BuffersHitBigMax  uint64 `yaml:"buffersHitBigMax"`

	// T3 ADD_LIMIT.
	AddLimitMinRows uint64 `yaml:"addLimitMinRows"`

	// T4 TEMP_BUF_WRITTEN.
	TempWrittenBlocksMin uint64 `yaml:"tempWrittenBlocksMin"`

	// T5 INDEX_INEFFICIENT_HIGH_FILTERED.
	IndexIneffHighFilteredMin uint64 `yaml:"indexIneffHighFilteredMin"`

	// T6 VACUUM_ANALYZE_NEEDED.
	VacuumAnalyzeNeededFetchesMin uint64 `yaml:"vacuumAnalyzeNeededFetchesMin"`
}

// Explain Processing.
func NewExplain(explainJson string, config ExplainConfig) (*Explain, error) {
	var explains []Explain

	err := json.NewDecoder(strings.NewReader(explainJson)).Decode(&explains)

	if err != nil {
		return nil, err
	}

	if len(explains) == 0 {
		return nil, errors.New("Empty explain")
	}

	// TODO(anatoly): Is it possible to have more than one explain?
	var ex = &explains[0]
	ex.processExplain()

	ex.Config = config

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

func (ex *Explain) GetTips() ([]Tip, error) {
	c := ex.Config
	p := c.Params

	var tips []Tip

	// T1: SeqScan used.
	hitReadBlocks := ex.SharedReadBlocks + ex.SharedHitBlocks
	if ex.ContainsSeqScan && hitReadBlocks > p.BuffersHitReadSeqScan {
		tip, err := c.getTipByCode(TIP_SEQSCAN_USED)
		if err != nil {
			return make([]Tip, 0), err
		}
		tips = append(tips, tip)
	}

	// T2: Buffers read too big.
	if ex.SharedReadBlocks > p.BuffersReadBigMax ||
		ex.SharedHitBlocks > p.BuffersHitBigMax {
		tip, err := c.getTipByCode(TIP_TOO_MUCH_DATA)
		if err != nil {
			return make([]Tip, 0), err
		}
		tips = append(tips, tip)
	}

	// T3: Add limit.
	if ex.ActualRows > p.AddLimitMinRows && ex.Plan.NodeType != Limit {
		tip, err := c.getTipByCode(TIP_ADD_LIMIT)
		if err != nil {
			return make([]Tip, 0), err
		}
		tips = append(tips, tip)
	}

	// T4: Temp buffers written.
	if ex.TempWrittenBlocks > p.TempWrittenBlocksMin {
		tip, err := c.getTipByCode(TIP_TEMP_BUF_WRITTEN)
		if err != nil {
			return make([]Tip, 0), err
		}
		tips = append(tips, tip)
	}

	// T5: Index inefficient high filtered.
	if ex.IndexIneffHighFiltered {
		tip, err := c.getTipByCode(TIP_INDEX_INEFFICIENT_HIGH_FILTERED)
		if err != nil {
			return make([]Tip, 0), err
		}
		tips = append(tips, tip)
	}

	// T6: Vacuum analyze needed.
	if ex.VacuumAnalyzeNeeded {
		tip, err := c.getTipByCode(TIP_VACUUM_ANALYZE_NEEDED)
		if err != nil {
			return make([]Tip, 0), err
		}
		tips = append(tips, tip)
	}

	return tips, nil
}

func (ex *Explain) processExplain() {
	ex.calculateParams()

	ex.processPlan(&ex.Plan)
	ex.calculateOutlierNodes(&ex.Plan)
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

	if plan.PlanRows == plan.ActualRows {
		return
	}

	plan.PlannerRowEstimateDirection = Under
	if plan.PlanRows != 0 {
		plan.PlannerRowEstimateFactor = float64(plan.ActualRows) / float64(plan.PlanRows)
	}

	if plan.PlannerRowEstimateFactor < 1.0 {
		plan.PlannerRowEstimateFactor = 0
		plan.PlannerRowEstimateDirection = Over
		if plan.ActualRows != 0 {
			plan.PlannerRowEstimateFactor = float64(plan.PlanRows) / float64(plan.ActualRows)
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
	p := ex.Config.Params

	plan.Costliest = plan.ActualCost == ex.MaxCost
	plan.Largest = plan.ActualRows == ex.MaxRows
	plan.Slowest = plan.ActualDuration == ex.MaxDuration

	if plan.NodeType == IndexScan && plan.RowsRemovedByFilter > p.IndexIneffHighFilteredMin {
		ex.IndexIneffHighFiltered = true
	}

	if plan.NodeType == IndexOnlyScan && plan.HeapFetches > p.VacuumAnalyzeNeededFetchesMin {
		ex.VacuumAnalyzeNeeded = true
	}

	for index := range plan.Plans {
		ex.calculateOutlierNodes(&plan.Plans[index])
	}
}

func (config *ExplainConfig) getTipByCode(code string) (Tip, error) {
	tips := config.Tips
	for _, tip := range tips {
		if tip.Code == code {
			return tip, nil
		}
	}

	return Tip{}, errors.New("Tip not found, check your explain config")
}

func (ex *Explain) writeExplainText(writer io.Writer) {
	ex.writePlanText(writer, &ex.Plan, " ", 0, len(ex.Plan.Plans) == 1)
}

func (ex *Explain) writeStatsText(writer io.Writer) {
	fmt.Fprintf(writer, "\nTime: %s\n", util.MillisecondsToString(ex.TotalTime))
	fmt.Fprintf(writer, "  - planning: %s\n", util.MillisecondsToString(ex.PlanningTime))
	fmt.Fprintf(writer, "  - execution: %s\n", util.MillisecondsToString(ex.ExecutionTime))
	fmt.Fprintf(writer, "    - I/O read: %s\n", util.MillisecondsToString(ex.IOReadTime))
	fmt.Fprintf(writer, "    - I/O write: %s\n", util.MillisecondsToString(ex.IOWriteTime))

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

func (ex *Explain) writePlanText(writer io.Writer, plan *Plan, prefix string, depth int, lastChild bool) {
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

	writePlanTextNodeCaption(outputFn, plan)

	currentPrefix = prefix + "  "
	if depth != 0 {
		currentPrefix = prefix + subplanPrefix + "      "
	}

	writePlanTextNodeDetails(outputFn, plan)

	for index := range plan.Plans {
		ex.writePlanText(writer, &plan.Plans[index], currentPrefix, depth+1, index == len(plan.Plans)-1)
	}
}

func writeSubplanTextNodeCaption(outputFn func(string, ...interface{}) (int, error), plan *Plan) {
	outputFn("%s", plan.SubplanName)
}

func writePlanTextNodeCaption(outputFn func(string, ...interface{}) (int, error), plan *Plan) {
	costs := fmt.Sprintf("(cost=%.2f..%.2f rows=%d width=%d)", plan.StartupCost, plan.TotalCost, plan.PlanRows, plan.PlanWidth)
	timing := fmt.Sprintf("(actual time=%.3f..%.3f rows=%d loops=%d)", plan.ActualStartupTime, plan.ActualTotalTime, plan.ActualRows, plan.ActualLoops)

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
	if plan.NodeType == ModifyTable { // E.g. for Insert.
		nodeType = plan.Operation
	}

	if plan.NodeType == HashJoin && plan.JoinType == "Left" {
		nodeType = "Hash Left Join"
	}

	if plan.NodeType == Aggregate && plan.Strategy == "Hashed" {
		nodeType = fmt.Sprintf("Hash%v", Aggregate)
	}

	if plan.NodeType == NestedLoop && plan.JoinType == "Left" {
		nodeType = fmt.Sprintf("%v %s Join", NestedLoop, plan.JoinType)
	}

	parallel := ""
	if plan.ParallelAware {
		parallel = "Parallel "
	}

	outputFn("%s%v%s%s  %v %v", parallel, nodeType, using, on, costs, timing)
}

func writePlanTextNodeDetails(outputFn func(string, ...interface{}) (int, error), plan *Plan) {
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

	if plan.NodeType == IndexOnlyScan {
		outputFn("Heap Fetches: %d", plan.HeapFetches)
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

	ioTiming := ""
	if plan.IOReadTime > 0 {
		ioTiming += fmt.Sprintf(" read=%.3f", plan.IOReadTime)
	}
	if plan.IOWriteTime > 0 {
		ioTiming += fmt.Sprintf(" write=%.3f", plan.IOWriteTime)
	}

	if len(ioTiming) > 0 {
		outputFn("I/O Timings:%s", ioTiming)
	}
}

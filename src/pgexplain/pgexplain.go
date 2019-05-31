/*
2019 © Anatoly Stansler anatoly@postgres.ai
2019 © Postgres.ai
Using code from Simon Engledew @ https://github.com/simon-engledew/gocmdpev
*/

package pgexplain

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/dustin/go-humanize"
	"github.com/mitchellh/go-wordwrap"
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
	Plan                Plan          `json:"Plan"`
	PlanningTime        float64       `json:"Planning Time"`
	Triggers            []interface{} `json:"Triggers"`
	ExecutionTime       float64       `json:"Execution Time"`
	TotalCost           float64
	MaxRows             uint64
	MaxCost             float64
	MaxDuration         float64
	ContainsSeqScan     bool
	SharedHitBlocks     uint64
	SharedWrittenBlocks uint64
	SharedReadBlocks    uint64
	Config              ExplainConfig `json:"-"`
}

type Plan struct {
	ActualCost                  float64
	ActualDuration              float64
	ActualLoops                 uint64  `json:"Actual Loops"`
	ActualRows                  uint64  `json:"Actual Rows"`
	ActualStartupTime           float64 `json:"Actual Startup Time"`
	ActualTotalTime             float64 `json:"Actual Total Time"`
	Alias                       string  `json:"Alias"`
	Costliest                   bool
	CteName                     string   `json:"CTE Name"`
	Filter                      string   `json:"Filter"`
	GroupKey                    []string `json:"Group Key"`
	HashCondition               string   `json:"Hash Cond"`
	HeapFetches                 uint64   `json:"Heap Fetches"`
	IndexCondition              string   `json:"Index Cond"`
	IndexName                   string   `json:"Index Name"`
	IOReadTime                  float64  `json:"I/O Read Time"`
	IOWriteTime                 float64  `json:"I/O Write Time"`
	JoinType                    string   `json:"Join Type"`
	Largest                     bool
	LocalDirtiedBlocks          uint64   `json:"Local Dirtied Blocks"`
	LocalHitBlocks              uint64   `json:"Local Hit Blocks"`
	LocalReadBlocks             uint64   `json:"Local Read Blocks"`
	LocalWrittenBlocks          uint64   `json:"Local Written Blocks"`
	NodeType                    NodeType `json:"Node Type"`
	Operation                   string   `json:"Operation"`
	Output                      []string `json:"Output"`
	ParentRelationship          string   `json:"Parent Relationship"`
	PlannerRowEstimateDirection EstimateDirection
	PlannerRowEstimateFactor    float64
	PlanRows                    uint64   `json:"Plan Rows"`
	Plans                       []Plan   `json:"Plans"`
	PlanWidth                   uint64   `json:"Plan Width"`
	RelationName                string   `json:"Relation Name"`
	RowsRemovedByFilter         uint64   `json:"Rows Removed by Filter"`
	RowsRemovedByIndexRecheck   uint64   `json:"Rows Removed by Index Recheck"`
	ScanDirection               string   `json:"Scan Direction"`
	Schema                      string   `json:"Schema"`
	SharedDirtiedBlocks         uint64   `json:"Shared Dirtied Blocks"`
	SharedHitBlocks             uint64   `json:"Shared Hit Blocks"`
	SharedReadBlocks            uint64   `json:"Shared Read Blocks"`
	SharedWrittenBlocks         uint64   `json:"Shared Written Blocks"`
	SubplanName                 string   `json:"Subplan Name"`
	SortKey                     []string `json:"Sort Key"`
	SortMethod                  string   `json:"Sort Method"`
	SortSpaceUsed               uint64   `json:"Sort Space Used"` // kB
	SortSpaceType               string   `json:"Sort Space Type"`
	HashBuckets                 uint64   `json:"Hash Buckets"`
	OriginalHashBuckets         uint64   `json:"Original Hash Buckets"`
	HashBatches                 uint64   `json:"Hash Batches"`
	OriginalHashBatches         uint64   `json:"Original Hash Batches"`
	PeakMemoryUsage             uint64   `json:"Peak Memory Usage"` // kB
	Slowest                     bool
	StartupCost                 float64 `json:"Startup Cost"`
	Strategy                    string  `json:"Strategy"`
	TempReadBlocks              uint64  `json:"Temp Read Blocks"`
	TempWrittenBlocks           uint64  `json:"Temp Written Blocks"`
	TotalCost                   float64 `json:"Total Cost"`
}

const (
	TIP_SEQSCAN_USED     = "SEQSCAN_USED"
	TIP_BUFFERS_READ_BIG = "BUFFERS_READ_BIG"
	TIP_BUFFERS_HIT_BIG  = "BUFFERS_HIT_BIG"
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

type ParamsConfig struct {
	BuffersReadBigMax uint64 `yaml:"buffersReadBigMax"`
	BuffersHitBigMax  uint64 `yaml:"buffersHitBigMax"`
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
	var explain = explains[0]
	processExplain(&explain)

	explain.Config = config

	return &explain, nil
}

func processExplain(explain *Explain) {
	calculateBuffers(explain)

	processPlan(explain, &explain.Plan)
	calculateOutlierNodes(explain, &explain.Plan)
}

func calculateBuffers(explain *Explain) {
	explain.SharedHitBlocks = explain.Plan.SharedHitBlocks
	explain.SharedWrittenBlocks = explain.Plan.SharedWrittenBlocks
	explain.SharedReadBlocks = explain.Plan.SharedReadBlocks
}

func processPlan(explain *Explain, plan *Plan) {
	checkSeqScan(explain, plan)
	calculatePlannerEstimate(explain, plan)
	calculateActuals(explain, plan)
	calculateMaximums(explain, plan)

	for index := range plan.Plans {
		processPlan(explain, &plan.Plans[index])
	}
}

func checkSeqScan(explain *Explain, plan *Plan) {
	explain.ContainsSeqScan = explain.ContainsSeqScan || plan.NodeType == SequenceScan
}

func calculatePlannerEstimate(explain *Explain, plan *Plan) {
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

func calculateActuals(explain *Explain, plan *Plan) {
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

	explain.TotalCost = explain.TotalCost + plan.ActualCost

	plan.ActualDuration = plan.ActualDuration * float64(plan.ActualLoops)
}

func calculateMaximums(explain *Explain, plan *Plan) {
	if explain.MaxRows < plan.ActualRows {
		explain.MaxRows = plan.ActualRows
	}
	if explain.MaxCost < plan.ActualCost {
		explain.MaxCost = plan.ActualCost
	}
	if explain.MaxDuration < plan.ActualDuration {
		explain.MaxDuration = plan.ActualDuration
	}
}

func calculateOutlierNodes(explain *Explain, plan *Plan) {
	plan.Costliest = plan.ActualCost == explain.MaxCost
	plan.Largest = plan.ActualRows == explain.MaxRows
	plan.Slowest = plan.ActualDuration == explain.MaxDuration

	for index := range plan.Plans {
		calculateOutlierNodes(explain, &plan.Plans[index])
	}
}

// Explain Recommendations.
func (e *Explain) GetTips() ([]Tip, error) {
	var tips []Tip

	// T1: SeqScan used.
	if e.ContainsSeqScan {
		tip, err := e.Config.getTipByCode(TIP_SEQSCAN_USED)
		if err != nil {
			return make([]Tip, 0), err
		}
		tips = append(tips, tip)
	}

	// T2: Buffers read too big.
	if e.SharedReadBlocks > 100 {
		tip, err := e.Config.getTipByCode(TIP_BUFFERS_READ_BIG)
		if err != nil {
			return make([]Tip, 0), err
		}
		tips = append(tips, tip)
	}

	// T3: Buffers hit too big.
	if e.SharedHitBlocks > 1000 {
		tip, err := e.Config.getTipByCode(TIP_BUFFERS_HIT_BIG)
		if err != nil {
			return make([]Tip, 0), err
		}
		tips = append(tips, tip)
	}

	return tips, nil
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

// Explain Visualization.
func (e *Explain) Visualize(writer io.Writer) {
	writeExplainText(writer, e)
}

func writeExplainText(writer io.Writer, explain *Explain) {
	writePlanText(writer, explain, &explain.Plan, " ", 0, len(explain.Plan.Plans) == 1)
	fmt.Fprintf(writer, " Planning time: %s\n", durationToString(explain.PlanningTime))
	fmt.Fprintf(writer, " Execution time: %s\n", durationToString(explain.ExecutionTime))
	fmt.Fprintf(writer, " Total Cost: %.2f\n", explain.TotalCost)
	fmt.Fprintf(writer, " Buffers Hit: %d\n", explain.SharedHitBlocks)
	fmt.Fprintf(writer, " Buffers Written: %d\n", explain.SharedWrittenBlocks)
	fmt.Fprintf(writer, " Buffers Read: %d\n", explain.SharedReadBlocks)
}

func writePlanText(writer io.Writer, explain *Explain, plan *Plan, prefix string, depth int, lastChild bool) {
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
		writePlanText(writer, explain, &plan.Plans[index], currentPrefix, depth+1, index == len(plan.Plans)-1)
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

	nodeType := fmt.Sprintf("%v", plan.NodeType)
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

	outputFn("%v%s%s  %v %v", nodeType, using, on, costs, timing)
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

	if plan.HashCondition != "" {
		outputFn("Hash Cond: %v", plan.HashCondition)
	}

	if plan.Filter != "" {
		outputFn("Filter: %v", plan.Filter)
		outputFn("Rows Removed by Filter: %d", plan.RowsRemovedByFilter)
	}

	buffers := ""
	if plan.SharedDirtiedBlocks > 0 || plan.SharedHitBlocks > 0 || plan.SharedReadBlocks > 0 || plan.SharedWrittenBlocks > 0 {
		buffers += "shared"
		if plan.SharedDirtiedBlocks > 0 {
			buffers += fmt.Sprintf(" dirtied=%d", plan.SharedDirtiedBlocks)
		}
		if plan.SharedHitBlocks > 0 {
			buffers += fmt.Sprintf(" hit=%d", plan.SharedHitBlocks)
		}
		if plan.SharedReadBlocks > 0 {
			buffers += fmt.Sprintf(" read=%d", plan.SharedReadBlocks)
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
		if plan.LocalDirtiedBlocks > 0 {
			buffers += fmt.Sprintf(" dirtied=%d", plan.LocalDirtiedBlocks)
		}
		if plan.LocalHitBlocks > 0 {
			buffers += fmt.Sprintf(" hit=%d", plan.LocalHitBlocks)
		}
		if plan.LocalReadBlocks > 0 {
			buffers += fmt.Sprintf(" read=%d", plan.LocalReadBlocks)
		}
		if plan.LocalWrittenBlocks > 0 {
			buffers += fmt.Sprintf(" written=%d", plan.LocalWrittenBlocks)
		}
	}

	if buffers != "" {
		outputFn("Buffers: %s", buffers)
	}
}

func (e *Explain) VisualizeTree(writer io.Writer) {
	writeExplainTree(writer, e)
}

func writeExplainTree(writer io.Writer, explain *Explain) {
	fmt.Fprintf(writer, "Total Cost: %s\n", humanize.Commaf(explain.TotalCost))
	fmt.Fprintf(writer, "Planning Time: %s\n", durationToString(explain.PlanningTime))
	fmt.Fprintf(writer, "Execution Time: %s\n", durationToString(explain.ExecutionTime))
	fmt.Fprintf(writer, "Buffers Hit: %d\n", explain.SharedHitBlocks)
	fmt.Fprintf(writer, "Buffers Written: %d\n", explain.SharedWrittenBlocks)
	fmt.Fprintf(writer, "Buffers Read: %d\n", explain.SharedReadBlocks)
	fmt.Fprintf(writer, "┬\n")

	writePlanTree(writer, explain, &explain.Plan, "", 0, len(explain.Plan.Plans) == 1)
}

func durationToString(value float64) string {
	if value < 1 {
		return "<1 ms"
	} else if value < 100 {
		return fmt.Sprintf("%.3f ms", value)
	} else if value < 1000 {
		return fmt.Sprintf("%.3f ms", value)
	} else if value < 60000 {
		return fmt.Sprintf("%.3f s", value/2000.0)
	} else {
		return fmt.Sprintf("%.3f m", value/60000.0)
	}
}

func formatDetails(plan *Plan) string {
	var details []string

	if plan.ScanDirection != "" {
		details = append(details, plan.ScanDirection)
	}

	if plan.Strategy != "" {
		details = append(details, plan.Strategy)
	}

	if len(details) > 0 {
		return fmt.Sprintf(" [%v]", strings.Join(details, ", "))
	}

	return ""
}

func formatTags(plan *Plan) string {
	var tags []string

	if plan.Slowest {
		tags = append(tags, "slowest")
	}
	if plan.Costliest {
		tags = append(tags, "costliest")
	}
	if plan.Largest {
		tags = append(tags, "largest")
	}
	if plan.PlannerRowEstimateFactor >= 100 {
		tags = append(tags, "bad estimate")
	}

	return strings.Join(tags, " ")
}

func getTerminator(index int, plan *Plan) string {
	if index == 0 {
		if len(plan.Plans) == 0 {
			return "⌡► "
		} else {
			return "├►  "
		}
	} else {
		if len(plan.Plans) == 0 {
			return "   "
		} else {
			return "│  "
		}
	}
}

func writePlanTree(writer io.Writer, explain *Explain, plan *Plan, prefix string, depth int, lastChild bool) {
	currentPrefix := prefix

	var outputFn = func(format string, a ...interface{}) (int, error) {
		return fmt.Fprintf(writer, fmt.Sprintf("%s%s\n", currentPrefix, format), a...)
	}

	outputFn("│")

	joint := "├"
	if len(plan.Plans) > 1 || lastChild {
		joint = "└"
	}

	outputFn("%v %v%v %v", joint+"─⌠", plan.NodeType, formatDetails(plan), formatTags(plan))

	if len(plan.Plans) > 1 || lastChild {
		prefix += "  "
	} else {
		prefix += "│ "
	}

	currentPrefix = prefix + "│ "

	outputFn("○ %v %v (%.0f%%)", "Duration:", durationToString(plan.ActualDuration), (plan.ActualDuration/explain.ExecutionTime)*100)

	outputFn("○ %v %v (%.0f%%)", "Cost:", humanize.Commaf(plan.ActualCost), (plan.ActualCost/explain.TotalCost)*100)

	outputFn("○ %v %v", "Rows:", humanize.Comma(int64(plan.ActualRows)))

	currentPrefix = currentPrefix + "  "

	if plan.JoinType != "" {
		outputFn("%v %v", plan.JoinType, "join")
	}

	if plan.RelationName != "" {
		outputFn("%v %v.%v", "on", plan.Schema, plan.RelationName)
	}

	if plan.IndexName != "" {
		outputFn("%v %v", "using", plan.IndexName)
	}

	if plan.IndexCondition != "" {
		outputFn("%v %v", "condition", plan.IndexCondition)
	}

	if plan.Filter != "" {
		outputFn("%v %v %v", "filter", plan.Filter, fmt.Sprintf("[-%v rows]", humanize.Comma(int64(plan.RowsRemovedByFilter))))
	}

	if plan.HashCondition != "" {
		outputFn("%v %v", "on", plan.HashCondition)
	}

	if plan.CteName != "" {
		outputFn("CTE %v", plan.CteName)
	}

	if plan.PlannerRowEstimateFactor != 0 {
		outputFn("%v %vestimated %v %.2fx", "rows", plan.PlannerRowEstimateDirection, "by", plan.PlannerRowEstimateFactor)
	}

	currentPrefix = prefix

	if len(plan.Output) > 0 {
		for index, line := range strings.Split(wordwrap.WrapString(strings.Join(plan.Output, " + "), 60), "\n") {
			outputFn(getTerminator(index, plan) + line)
		}
	}

	for index := range plan.Plans {
		writePlanTree(writer, explain, &plan.Plans[index], prefix, depth+1, index == len(plan.Plans)-1)
	}
}

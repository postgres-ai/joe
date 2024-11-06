package main

import (
    "bufio"
    "encoding/json"
    "fmt"
    "io"
    "os"
    "strings"
)

type Plan struct {
    NodeType       string   `json:"Node Type,omitempty"`
    Strategy       string   `json:"Strategy,omitempty"`
    PartialMode    string   `json:"Partial Mode,omitempty"`
    ParallelAware bool     `json:"Parallel Aware"`
    AsyncCapable  bool     `json:"Async Capable"`
    StartupCost   float64  `json:"Startup Cost"`
    TotalCost     float64  `json:"Total Cost"`
    PlanRows      int64    `json:"Plan Rows"`
    PlanWidth     int64    `json:"Plan Width"`
    
    // Actual execution statistics
    ActualStartupTime float64 `json:"Actual Startup Time"`
    ActualTotalTime   float64 `json:"Actual Total Time"`
    ActualRows        int64   `json:"Actual Rows"`
    ActualLoops       int64   `json:"Actual Loops"`
    
    // Node-specific attributes
    RelationName    string   `json:"Relation Name,omitempty"`
    Schema          string   `json:"Schema,omitempty"`
    Alias           string   `json:"Alias,omitempty"`
    IndexName       string   `json:"Index Name,omitempty"`
    IndexCond       string   `json:"Index Cond,omitempty"`
    RecheckCond     string   `json:"Recheck Cond,omitempty"`
    JoinType        string   `json:"Join Type,omitempty"`
    ParentRelationship string `json:"Parent Relationship,omitempty"`
    Output          []string `json:"Output,omitempty"`
    
    // Filter conditions
    Filter string `json:"Filter,omitempty"`
    RowsRemovedByFilter int64 `json:"Rows Removed by Filter"`
    
    // Buffer statistics
    SharedHitBlocks     int64 `json:"Shared Hit Blocks"`
    SharedReadBlocks    int64 `json:"Shared Read Blocks"`
    SharedDirtiedBlocks int64 `json:"Shared Dirtied Blocks"`
    SharedWrittenBlocks int64 `json:"Shared Written Blocks"`
    LocalHitBlocks      int64 `json:"Local Hit Blocks"`
    LocalReadBlocks     int64 `json:"Local Read Blocks"`
    LocalDirtiedBlocks  int64 `json:"Local Dirtied Blocks"`
    LocalWrittenBlocks  int64 `json:"Local Written Blocks"`
    TempReadBlocks      int64 `json:"Temp Read Blocks"`
    TempWrittenBlocks   int64 `json:"Temp Written Blocks"`
    
    // I/O Timing
    IOReadTime  float64 `json:"I/O Read Time"`
    IOWriteTime float64 `json:"I/O Write Time"`
    
    // Nested plans
    Plans []Plan `json:"Plans,omitempty"`
}

type Explain struct {
    PlanningTime  float64 `json:"Planning Time"`
    ExecutionTime float64 `json:"Execution Time"`
    Plan          Plan    `json:"Plan"`
}

func cleanJSON(input string) string {
    // Replace double quotes with single quotes
    input = strings.ReplaceAll(input, `""`, `"`)
    return input
}

func writePlan(w io.Writer, plan *Plan, level int) {
    indent := strings.Repeat("  ", level)
    
    // Build node description
    nodeName := plan.NodeType
    if plan.Strategy != "" && plan.Strategy != "Plain" {
        nodeName = fmt.Sprintf("%s %s", plan.Strategy, nodeName)
    }
    
    if plan.RelationName != "" {
        if plan.Schema != "" {
            nodeName = fmt.Sprintf("%s on %s.%s", nodeName, plan.Schema, plan.RelationName)
        } else {
            nodeName = fmt.Sprintf("%s on %s", nodeName, plan.RelationName)
        }
        if plan.Alias != "" && plan.Alias != plan.RelationName {
            nodeName = fmt.Sprintf("%s %s", nodeName, plan.Alias)
        }
    }

    if plan.JoinType != "" && plan.JoinType != "Inner" {
        nodeName = fmt.Sprintf("%s %s", nodeName, plan.JoinType)
    }
    
    fmt.Fprintf(w, "%s%s  (cost=%.2f..%.2f rows=%d width=%d) (actual time=%.3f..%.3f rows=%d loops=%d)\n",
        indent, nodeName,
        plan.StartupCost, plan.TotalCost, plan.PlanRows, plan.PlanWidth,
        plan.ActualStartupTime, plan.ActualTotalTime, plan.ActualRows, plan.ActualLoops)
    
    if plan.IndexName != "" {
        fmt.Fprintf(w, "%s  Index: %s\n", indent, plan.IndexName)
    }
    if plan.IndexCond != "" {
        fmt.Fprintf(w, "%s  Index Cond: %s\n", indent, plan.IndexCond)
    }
    if plan.RecheckCond != "" {
        fmt.Fprintf(w, "%s  Recheck Cond: %s\n", indent, plan.RecheckCond)
    }
    if plan.Filter != "" {
        fmt.Fprintf(w, "%s  Filter: %s\n", indent, plan.Filter)
        if plan.RowsRemovedByFilter > 0 {
            fmt.Fprintf(w, "%s  Rows Removed by Filter: %d\n", indent, plan.RowsRemovedByFilter)
        }
    }
    
    if plan.SharedHitBlocks > 0 || plan.SharedReadBlocks > 0 {
        fmt.Fprintf(w, "%s  Buffers: shared hit=%d read=%d\n",
            indent, plan.SharedHitBlocks, plan.SharedReadBlocks)
    }

    if plan.IOReadTime > 0 || plan.IOWriteTime > 0 {
        fmt.Fprintf(w, "%s  I/O Timings: read=%.3f write=%.3f\n",
            indent, plan.IOReadTime, plan.IOWriteTime)
    }
    
    for _, child := range plan.Plans {
        writePlan(w, &child, level+1)
    }
}

func renderPlan(explain *Explain) string {
    var buf strings.Builder
    writePlan(&buf, &explain.Plan, 0)
    fmt.Fprintf(&buf, "\nPlanning Time: %.3f ms\n", explain.PlanningTime)
    fmt.Fprintf(&buf, "Execution Time: %.3f ms\n", explain.ExecutionTime)
    return buf.String()
}

func main() {
    fmt.Println("Paste your PostgreSQL explain plan and type ';;;' on a new line when done:")
    fmt.Println("(The plan should start with { and end with })")
    
    scanner := bufio.NewScanner(os.Stdin)
    var input strings.Builder
    
    for scanner.Scan() {
        line := scanner.Text()
        if line == ";;;" {
            break
        }
        input.WriteString(line)
    }
    
    if err := scanner.Err(); err != nil {
        fmt.Fprintf(os.Stderr, "Error reading input: %v\n", err)
        os.Exit(1)
    }

    if input.Len() > 0 {
        planStr := input.String()
        
        // Clean up the JSON
        cleanedJSON := cleanJSON(planStr)
        
        fmt.Printf("Input JSON length: %d\n", len(planStr))
        fmt.Printf("Cleaned JSON length: %d\n", len(cleanedJSON))
        fmt.Println("\nFirst 500 chars of cleaned JSON:")
        if len(cleanedJSON) > 500 {
            fmt.Println(cleanedJSON[:500])
        } else {
            fmt.Println(cleanedJSON)
        }

        // Parse the explain plan
        var explain Explain
        if err := json.Unmarshal([]byte(cleanedJSON), &explain); err != nil {
            fmt.Fprintf(os.Stderr, "Error parsing explain plan: %v\n", err)
            os.Exit(1)
        }

        // Debug output
        fmt.Printf("\nParsed Plan Node Type: %s\n", explain.Plan.NodeType)
        fmt.Printf("Parsed Plan Strategy: %s\n", explain.Plan.Strategy)
        fmt.Printf("Parsed Planning Time: %f\n", explain.PlanningTime)

        fmt.Println("\nExecution Plan:")
        fmt.Println(renderPlan(&explain))
    }
}

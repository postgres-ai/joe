/*
2021 © Postgres.ai
Based on the code from Alexey Lesovsky (lesovsky <at> gmail.com) @ https://github.com/lesovsky/pgcenter
*/

package estimator

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgtype/pgxtype"
	"github.com/jackc/pgx/v4"

	"gitlab.com/postgres-ai/database-lab/pkg/log"
)

const (
	query = `SELECT
				extract(epoch from clock_timestamp() - query_start) AS query_duration,
				date_trunc('milliseconds', state_change) AS state_change_time,
				state AS state,
				wait_event_type ||'.'|| wait_event AS wait_entry,
				query
			FROM pg_stat_activity WHERE pid = $1 /* pgcenter profile */`

	waitForBackendActivity = 2 * time.Millisecond
)

// waitEvent defines an auxiliary struct to sort events.
type waitEvent struct {
	waitEventName  string
	waitEventValue float64
}

// TraceStat describes data retrieved from Postgres' pg_stat_activity view.
type TraceStat struct {
	queryDurationSec sql.NullFloat64
	stateChangeTime  sql.NullString
	state            sql.NullString
	waitEntry        sql.NullString
	queryText        sql.NullString
}

// TraceOptions defines program's configuration options.
type TraceOptions struct {
	Pid      int           // PID of profiled backend.
	Interval time.Duration // Profiling interval.
	StrSize  int           // Limit length for query string.
}

// Profiler defines a profiling structure.
type Profiler struct {
	conn               pgxtype.Querier
	opts               TraceOptions
	out                strings.Builder
	waitEventDurations map[string]float64 // wait events and its durations.
	waitEventPercents  map[string]float64 // wait events and its percent ratios.
	sampleCounter      int
	exitChan           chan struct{}
}

// NewProfiler creates a new profiler.
func NewProfiler(conn pgxtype.Querier, opts TraceOptions) *Profiler {
	return &Profiler{
		conn:               conn,
		opts:               opts,
		waitEventDurations: make(map[string]float64),
		waitEventPercents:  make(map[string]float64),
		exitChan:           make(chan struct{}),
	}
}

// Start runs the main profiling loop.
func (p *Profiler) Start(ctx context.Context) {
	prev, curr := TraceStat{}, TraceStat{}
	startup := true

	log.Dbg(fmt.Sprintf("Profiling process %d with %s sampling\n", p.opts.Pid, p.opts.Interval))

	for {
		row := p.conn.QueryRow(ctx, query, p.opts.Pid)
		err := row.Scan(&curr.queryDurationSec,
			&curr.stateChangeTime,
			&curr.state,
			&curr.waitEntry,
			&curr.queryText)

		if err != nil {
			if err == pgx.ErrNoRows {
				// print collected stats before exit
				p.printStat()
				log.Dbg(fmt.Sprintf("Process with pid %d doesn't exist (%s)\n", p.opts.Pid, err))
				log.Dbg("Stop profiling\n")

				break
			}

			log.Err(fmt.Sprintf("failed to scan row: %s\n", err))

			break
		}

		// Start collecting stats immediately if query is executing, otherwise waiting when query starts
		if startup {
			if curr.state.String == "active" {
				p.printHeader()
				p.countWaitings(curr, prev)

				startup = false
				prev = curr

				continue
			} else { /* waiting until backend becomes active */
				prev = curr
				startup = false
				time.Sleep(waitForBackendActivity)

				continue
			}
		}

		// Backend's state is changed, it means query is started or finished
		if curr.stateChangeTime != prev.stateChangeTime {
			// transition to active state -- query started -- reset stats and print header with query text
			if curr.state.String == "active" {
				p.resetCounters()
				p.printHeader()
			}
			// transition from active state -- query finished -- print collected stats and reset it
			if prev.state.String == "active" {
				p.printStat()
				p.resetCounters()
			}
		} else {
			// otherwise just count stats and sleep
			p.countWaitings(curr, prev)
			time.Sleep(p.opts.Interval)
		}

		// copy current stats snapshot to previous
		prev = curr
	}

	p.exitChan <- struct{}{}
}

// Finish returns a channel that's receiving data when profiling done.
func (p *Profiler) Finish() chan struct{} {
	return p.exitChan
}

// Count wait events durations and percent rations
func (p *Profiler) countWaitings(curr TraceStat, prev TraceStat) {
	event := curr.waitEntry.String

	if curr.waitEntry.String == "" {
		event = "Running"
	}

	/* calculate durations for collected wait events */
	p.waitEventDurations[event] += (curr.queryDurationSec.Float64 - prev.queryDurationSec.Float64)

	/* calculate percents */
	for k, v := range p.waitEventDurations {
		p.waitEventPercents[k] = (100 * v) / curr.queryDurationSec.Float64
	}

	p.sampleCounter++
}

// resetCounters deletes all entries from the maps.
func (p *Profiler) resetCounters() {
	p.waitEventDurations = make(map[string]float64)
	p.waitEventPercents = make(map[string]float64)
}

// CountSamples returns a number of samples.
func (p *Profiler) CountSamples() int {
	return p.sampleCounter
}

// TotalTime returns a total time of profiling events.
func (p *Profiler) TotalTime() float64 {
	var totalTime float64

	for _, duration := range p.waitEventDurations {
		totalTime += duration
	}

	return totalTime
}

// WaitEventsRatio returns a ratio of wait events.
func (p *Profiler) WaitEventsRatio() map[string]float64 {
	waitEvents := make(map[string]float64, len(p.waitEventPercents))

	for event, percent := range p.waitEventPercents {
		waitEvents[event] = percent
	}

	return waitEvents
}

// RenderStat renders the collected profiler stats.
func (p *Profiler) RenderStat() string {
	return p.out.String()
}

// printHeader prints stats header.
func (p *Profiler) printHeader() {
	p.out.WriteString(fmt.Sprintf("%% time      seconds wait_event\n"))
	p.out.WriteString("------ ------------ -----------------------------\n")
}

// printStat prints collected stats: wait events durations and percent ratios.
func (p *Profiler) printStat() {
	if len(p.waitEventDurations) == 0 {
		return
	} // nothing to do

	var totalPct, totalTime float64

	eventsList := make([]waitEvent, 0, len(p.waitEventDurations))

	for k, v := range p.waitEventDurations {
		eventsList = append(eventsList, waitEvent{k, v})
	}

	// Sort wait events by percent ratios.
	sort.Slice(eventsList, func(i, j int) bool {
		return eventsList[i].waitEventValue > eventsList[j].waitEventValue
	})

	// Print stats and calculating totals.
	for _, e := range eventsList {
		p.out.WriteString(fmt.Sprintf("%-*.2f %*.6f %s\n", 6, p.waitEventPercents[e.waitEventName], 12, e.waitEventValue, e.waitEventName))
		totalPct += p.waitEventPercents[e.waitEventName]
		totalTime += e.waitEventValue
	}

	// Print totals.
	p.out.WriteString("------ ------------ -----------------------------\n")
	p.out.WriteString(fmt.Sprintf("%-*.2f %*.6f\n", 6, totalPct, 12, totalTime))
}

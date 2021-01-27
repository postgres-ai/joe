/*
2021 © Postgres.ai
*/

// Package estimator provides tools to estimate query timing for a production environment.
package estimator

import (
	"fmt"
)

var readEvents = map[string]struct{}{
	"IO.BufFileRead":                  {},
	"IO.ControlFileRead":              {},
	"IO.CopyFileRead":                 {},
	"IO.DataFilePrefetch":             {},
	"IO.DataFileRead":                 {},
	"IO.LockFileAddToDataDirRead":     {},
	"IO.LockFileCreateRead":           {},
	"IO.LockFileReCheckDataDirRead":   {},
	"IO.RelationMapRead":              {},
	"IO.ReorderBufferRead":            {},
	"IO.ReorderLogicalMappingRead":    {},
	"IO.ReplicationSlotRead":          {},
	"IO.SLRURead":                     {},
	"IO.SnapbuildRead":                {},
	"IO.TimelineHistoryRead":          {},
	"IO.TwophaseFileRead":             {},
	"IO.WALCopyRead":                  {},
	"IO.WALRead":                      {},
	"IO.WALSenderTimelineHistoryRead": {},
}

var writeEvents = map[string]struct{}{
	"IO.BufFileWrite":                 {},
	"IO.ControlFileSync":              {},
	"IO.ControlFileSyncUpdate":        {},
	"IO.ControlFileWrite":             {},
	"IO.ControlFileWriteUpdate":       {},
	"IO.CopyFileWrite":                {},
	"IO.DSMFillZeroWrite":             {},
	"IO.DataFileExtend":               {},
	"IO.DataFileFlush":                {},
	"IO.DataFileImmediateSync":        {},
	"IO.DataFileSync":                 {},
	"IO.DataFileTruncate":             {},
	"IO.DataFileWrite":                {},
	"IO.LockFileAddToDataDirSync":     {},
	"IO.LockFileAddToDataDirWrite":    {},
	"IO.LockFileCreateSync":           {},
	"IO.LockFileCreateWrite":          {},
	"IO.LogicalRewriteCheckpointSync": {},
	"IO.LogicalRewriteMappingSync":    {},
	"IO.LogicalRewriteMappingWrite":   {},
	"IO.LogicalRewriteSync":           {},
	"IO.LogicalRewriteTruncate":       {},
	"IO.LogicalRewriteWrite":          {},
	"IO.RelationMapSync":              {},
	"IO.RelationMapWrite":             {},
	"IO.ReorderBufferWrite":           {},
	"IO.ReplicationSlotRestoreSync":   {},
	"IO.ReplicationSlotSync":          {},
	"IO.ReplicationSlotWrite":         {},
	"IO.SLRUFlushSync":                {},
	"IO.SLRUSync":                     {},
	"IO.SLRUWrite":                    {},
	"IO.SnapbuildSync":                {},
	"IO.SnapbuildWrite":               {},
	"IO.TimelineHistoryFileSync":      {},
	"IO.TimelineHistoryFileWrite":     {},
	"IO.TimelineHistorySync":          {},
	"IO.TimelineHistoryWrite":         {},
	"IO.TwophaseFileSync":             {},
	"IO.TwophaseFileWrite":            {},
	"IO.WALBootstrapSync":             {},
	"IO.WALBootstrapWrite":            {},
	"IO.WALCopySync":                  {},
	"IO.WALCopyWrite":                 {},
	"IO.WALInitSync":                  {},
	"IO.WALInitWrite":                 {},
	"IO.WALSync":                      {},
	"IO.WALSyncMethodAssign":          {},
	"IO.WALWrite":                     {},
}

func isReadEvent(event string) bool {
	_, ok := readEvents[event]

	return ok
}

func isWriteEvent(event string) bool {
	_, ok := writeEvents[event]

	return ok
}

// delta defines insignificant difference between minimum and maximum values.
const delta = 0.05

// Timing defines a timing estimator.
type Timing struct {
	dbStat          *StatDatabase
	readPercentage  float64
	writePercentage float64
	normal          float64
	readRatio       float64
	writeRatio      float64
	readBlocks      uint64
}

// StatDatabase defines database blocks stats.
type StatDatabase struct {
	BlocksRead     int64   `json:"blks_read"`
	BlocksHit      int64   `json:"blks_hit"`
	BlockReadTime  float64 `json:"blk_read_time"`
	BlockWriteTime float64 `json:"blk_write_time"`
}

// NewTiming creates a new timing estimator.
func NewTiming(waitEvents map[string]float64, readRatio, writeRatio float64) *Timing {
	timing := &Timing{
		readRatio:  readRatio,
		writeRatio: writeRatio,
	}

	for event, percent := range waitEvents {
		switch {
		case isReadEvent(event):
			timing.readPercentage += percent

		case isWriteEvent(event):
			timing.writePercentage += percent

		default:
			timing.normal += percent
		}
	}

	return timing
}

// SetDBStat sets database stats.
func (est *Timing) SetDBStat(dbStat StatDatabase) {
	est.dbStat = &dbStat
}

// SetReadBlocks sets read blocks.
func (est *Timing) SetReadBlocks(readBlocks uint64) {
	est.readBlocks = readBlocks
}

// CalcMin calculates the minimum query time estimation for the production environment, given the prepared ratios.
func (est *Timing) CalcMin(elapsed float64) float64 {
	return (est.normal + est.writePercentage/est.writeRatio) / 100 * elapsed
}

// CalcMax calculates the maximum query time estimation for the production environment, given the prepared ratios.
func (est *Timing) CalcMax(elapsed float64) float64 {
	readPercentage := est.readPercentage

	if est.dbStat != nil && est.readBlocks != 0 {
		readSpeed := float64(est.dbStat.BlocksRead) / (est.dbStat.BlockReadTime / 1000)
		readPercentage = float64(est.readBlocks) / readSpeed
	}

	return (est.normal + readPercentage/est.readRatio + est.writePercentage/est.writeRatio) / 100 * elapsed
}

// EstTime prints estimation timings.
func (est *Timing) EstTime(elapsed float64) string {
	minTiming := est.CalcMin(elapsed)
	maxTiming := est.CalcMax(elapsed)

	estTime := fmt.Sprintf("%.3f...%.3f", minTiming, maxTiming)

	if maxTiming-minTiming <= delta {
		estTime = fmt.Sprintf("%.3f", maxTiming)
	}

	return fmt.Sprintf(" (estimated* for prod: %s s)", estTime)
}

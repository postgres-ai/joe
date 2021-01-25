/*
2021 © Postgres.ai
*/

// Package estimator provides tools to estimate query timing for a production environment.
package estimator

import (
	"gitlab.com/postgres-ai/joe/pkg/util/operator"
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

// Timing defines a timing estimator.
type Timing struct {
	readPercentage  float64
	writePercentage float64
	normal          float64
	readRatio       float64
	writeRatio      float64
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

// CalcMin calculates the minimum query time for the production environment, given the prepared ratios.
func (est *Timing) CalcMin(elapsed float64) float64 {
	return (est.normal + est.readPercentage + est.writePercentage/est.writeRatio) / 100 * elapsed
}

// CalcMax calculates the maximum query time for the production environment, given the prepared ratios.
func (est *Timing) CalcMax(op string, stat StatDatabase, readBuf uint64, elapsed float64) float64 {
	rt := est.readPercentage

	if operator.IsDML(op) {
		readSpeed := float64(stat.BlocksRead) / stat.BlockReadTime
		rt = float64(readBuf) / readSpeed
	}

	return (est.normal + rt/est.readRatio + est.writePercentage/est.writeRatio) / 100 * elapsed
}

// StatDatabase ... .
type StatDatabase struct {
	BlockWriteTime float64 `json:"blk_write_time"`
	BlocksRead     int64   `json:"blks_read"`
	BlocksHit      int64   `json:"blks_hit"`
	BlockReadTime  float64 `json:"blk_read_time"`
}

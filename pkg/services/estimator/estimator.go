/*
2021 © Postgres.ai
*/

// Package estimator provides tools to estimate query timing for a production environment.
package estimator

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

// CalcTiming calculates the query time for the production environment, given the prepared factors.
func CalcTiming(waitEvents map[string]float64, readFactor, writeFactor, elapsed float64) float64 {
	var readRatio, writeRatio, normal float64

	for event, percent := range waitEvents {
		switch {
		case isReadEvent(event):
			readRatio += percent

		case isWriteEvent(event):
			writeRatio += percent

		default:
			normal += percent
		}
	}

	return (normal + readRatio/readFactor + writeRatio/writeFactor) / 100 * elapsed
}

/*
2026 © Postgres.ai
*/

package pgexplain

import (
	"strings"
	"testing"

	"github.com/AlekSi/pointer"
	"github.com/stretchr/testify/require"
)

// The expected lines follow PostgreSQL's FORMAT TEXT output, checked against
// live 13-18 servers with track_io_timing on: zero timings and empty sections
// are omitted, 13/14 print no section label, 15/16 print "shared" and "temp",
// and 17+ print "shared", "local", and "temp".
func TestFormatIOTiming(t *testing.T) {
	f := pointer.ToFloat64

	testCases := []struct {
		name string
		plan Plan
		want string
	}{
		{name: "track_io_timing off", plan: Plan{}, want: ""},

		{name: "13/14 zero", plan: Plan{IOReadTime: f(0), IOWriteTime: f(0)}, want: ""},
		{name: "13/14 read", plan: Plan{IOReadTime: f(1.5), IOWriteTime: f(0)}, want: " read=1.500"},
		{name: "13/14 read and write", plan: Plan{IOReadTime: f(1.5), IOWriteTime: f(0.25)}, want: " read=1.500 write=0.250"},

		{
			name: "15/16 zero",
			plan: Plan{IOReadTime: f(0), IOWriteTime: f(0), TempIOReadTime: f(0), TempIOWriteTime: f(0)},
			want: "",
		},
		{
			name: "15/16 shared and temp",
			plan: Plan{IOReadTime: f(1.5), IOWriteTime: f(0), TempIOReadTime: f(2.25), TempIOWriteTime: f(3)},
			want: " shared read=1.500, temp read=2.250 write=3.000",
		},
		{
			name: "15/16 temp only",
			plan: Plan{IOReadTime: f(0), IOWriteTime: f(0), TempIOReadTime: f(0), TempIOWriteTime: f(1)},
			want: " temp write=1.000",
		},

		{
			name: "17+ zero",
			plan: Plan{
				SharedIOReadTime: f(0), SharedIOWriteTime: f(0),
				LocalIOReadTime: f(0), LocalIOWriteTime: f(0),
				TempIOReadTime: f(0), TempIOWriteTime: f(0),
			},
			want: "",
		},
		{
			name: "17+ shared write",
			plan: Plan{
				SharedIOReadTime: f(0), SharedIOWriteTime: f(0.75),
				LocalIOReadTime: f(0), LocalIOWriteTime: f(0),
				TempIOReadTime: f(0), TempIOWriteTime: f(0),
			},
			want: " shared write=0.750",
		},
		{
			name: "17+ local",
			plan: Plan{
				SharedIOReadTime: f(0), SharedIOWriteTime: f(0),
				LocalIOReadTime: f(1), LocalIOWriteTime: f(2),
				TempIOReadTime: f(0), TempIOWriteTime: f(0),
			},
			want: " local read=1.000 write=2.000",
		},
		{
			name: "17+ all sections",
			plan: Plan{
				SharedIOReadTime: f(1.5), SharedIOWriteTime: f(0),
				LocalIOReadTime: f(0.5), LocalIOWriteTime: f(0),
				TempIOReadTime: f(2.25), TempIOWriteTime: f(3),
			},
			want: " shared read=1.500, local read=0.500, temp read=2.250 write=3.000",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, formatIOTiming(&tc.plan))
		})
	}
}

// TestRenderBufferIOAndWALLines checks the per-node lines after Buffers: like
// show_buffer_usage followed by show_wal_usage, I/O Timings comes before WAL,
// and WAL prints only positive counters.
func TestRenderBufferIOAndWALLines(t *testing.T) {
	const planJSON = `[{
		"Plan": {
			"Node Type": "ModifyTable", "Operation": "Insert", "Relation Name": "w", "Alias": "w",
			"Startup Cost": 0.00, "Total Cost": 1.00, "Plan Rows": 1, "Plan Width": 0,
			"Actual Startup Time": 1.000, "Actual Total Time": 1.000, "Actual Rows": 0.00, "Actual Loops": 1,
			"Shared Hit Blocks": 10, "Shared Read Blocks": 0, "Shared Dirtied Blocks": 2, "Shared Written Blocks": 0,
			"Local Hit Blocks": 0, "Local Read Blocks": 0, "Local Dirtied Blocks": 0, "Local Written Blocks": 0,
			"Temp Read Blocks": 0, "Temp Written Blocks": 0,
			"Shared I/O Read Time": 0.000, "Shared I/O Write Time": 0.120,
			"Local I/O Read Time": 0.000, "Local I/O Write Time": 0.000,
			"Temp I/O Read Time": 0.000, "Temp I/O Write Time": 0.000,
			"WAL Records": 20000, "WAL FPI": 0, "WAL Bytes": 11260000, "WAL Buffers Full": 1386
		},
		"Planning Time": 0.1, "Execution Time": 1.1
	}]`

	explain, err := NewExplain(planJSON)
	require.NoError(t, err)

	lines := strings.Split(explain.RenderPlanText(), "\n")
	require.GreaterOrEqual(t, len(lines), 4)

	require.Contains(t, lines[0], "(actual time=1.000..1.000 rows=0.00 loops=1)")
	require.Equal(t, "   Buffers: shared hit=10 dirtied=2", lines[1])
	require.Equal(t, "   I/O Timings: shared write=0.120", lines[2])
	require.Equal(t, "   WAL: records=20000 bytes=11260000 buffers full=1386", lines[3])
}

func TestRenderWALOmitsZeroCounters(t *testing.T) {
	require.Equal(t, "", appendPositiveCounter("", "fpi", 0))

	explain, err := NewExplain(`[{
		"Plan": {
			"Node Type": "Result", "Actual Rows": 1, "Actual Loops": 1,
			"WAL Records": 0, "WAL FPI": 0, "WAL Bytes": 0
		},
		"Planning Time": 0.1, "Execution Time": 0.1
	}]`)
	require.NoError(t, err)
	require.NotContains(t, explain.RenderPlanText(), "WAL:")
}

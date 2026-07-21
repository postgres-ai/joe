/*
2026 © Postgres.ai
*/

package msgproc

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeRowStream produces `total` rows through a REUSED value buffer, exactly
// like pgconn's ResultReader does on the wire: a row's raw values are only
// valid until the next NextRow call. Any implementation that materializes
// raw row references instead of converting row-by-row produces corrupted
// (last-row) values here — which is the point: collectV2ExecResult must
// stream, never buffer the full result set (SB2 OOM).
type fakeRowStream struct {
	fields []pgconn.FieldDescription
	total  int
	tag    pgconn.CommandTag

	produced int
	buf      [][]byte
	closed   bool
}

func (f *fakeRowStream) NextRow() bool {
	if f.produced >= f.total {
		return false
	}

	f.produced++

	value := strconv.Itoa(f.produced)

	if f.buf == nil {
		f.buf = [][]byte{make([]byte, 0, 32)}
	}

	// Overwrite the SAME backing buffer (wire-buffer reuse).
	f.buf[0] = append(f.buf[0][:0], value...)

	return true
}

func (f *fakeRowStream) FieldDescriptions() []pgconn.FieldDescription { return f.fields }

func (f *fakeRowStream) Values() [][]byte { return f.buf }

func (f *fakeRowStream) Close() (pgconn.CommandTag, error) {
	f.closed = true
	return f.tag, nil
}

// fakeResultStream is a v2 multi-result stream over fake row streams.
type fakeResultStream struct {
	results []*fakeRowStream
	next    int
	closed  bool
}

func (f *fakeResultStream) NextResult() bool {
	if f.next >= len(f.results) {
		return false
	}

	f.next++

	return true
}

func (f *fakeResultStream) ResultReader() v2RowStream { return f.results[f.next-1] }

func (f *fakeResultStream) Close() error {
	f.closed = true
	return nil
}

func TestCollectV2ExecResultStreamsAndCaps(t *testing.T) {
	idFields := []pgconn.FieldDescription{{Name: "n", DataTypeOID: pgtype.Int4OID}}

	t.Run("oversized result truncated at cap, fully counted, never materialized", func(t *testing.T) {
		total := v2ResultRowsCap + 500
		stream := &fakeResultStream{results: []*fakeRowStream{{fields: idFields, total: total}}}

		rows, rowCount, err := collectV2ExecResult(stream, v2ResultRowsCap)
		require.NoError(t, err)

		assert.Len(t, rows, v2ResultRowsCap, "result_rows must be capped")
		assert.Equal(t, total, rowCount, "row_count must still report the full size")

		// Buffer-reuse proof: values must have been converted row-by-row.
		assert.Equal(t, map[string]interface{}{"n": json.Number("1")}, rows[0])
		assert.Equal(t, map[string]interface{}{"n": json.Number(strconv.Itoa(v2ResultRowsCap))},
			rows[v2ResultRowsCap-1])

		assert.True(t, stream.results[0].closed)
		assert.True(t, stream.closed)
	})

	t.Run("last result set wins", func(t *testing.T) {
		stream := &fakeResultStream{results: []*fakeRowStream{
			{fields: idFields, total: 5},
			{fields: idFields, total: 2},
		}}

		rows, rowCount, err := collectV2ExecResult(stream, v2ResultRowsCap)
		require.NoError(t, err)
		assert.Len(t, rows, 2)
		assert.Equal(t, 2, rowCount)
	})

	t.Run("fieldless command reports rows affected", func(t *testing.T) {
		stream := &fakeResultStream{results: []*fakeRowStream{
			{tag: pgconn.NewCommandTag("UPDATE 7")},
		}}

		rows, rowCount, err := collectV2ExecResult(stream, v2ResultRowsCap)
		require.NoError(t, err)
		assert.Empty(t, rows)
		assert.Equal(t, 7, rowCount)
	})

	t.Run("no results", func(t *testing.T) {
		stream := &fakeResultStream{}

		rows, rowCount, err := collectV2ExecResult(stream, v2ResultRowsCap)
		require.NoError(t, err)
		assert.Equal(t, []interface{}{}, rows)
		assert.Equal(t, 0, rowCount)
	})
}

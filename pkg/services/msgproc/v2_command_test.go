/*
2026 © Postgres.ai
*/

package msgproc

import (
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
)

func TestConvertV2Value(t *testing.T) {
	cases := []struct {
		name     string
		oid      uint32
		raw      []byte
		expected interface{}
	}{
		{"sql null", pgtype.TextOID, nil, nil},
		{"bool true", pgtype.BoolOID, []byte("t"), true},
		{"bool false", pgtype.BoolOID, []byte("f"), false},
		{"int4", pgtype.Int4OID, []byte("42"), json.Number("42")},
		{"int8 beyond float64", pgtype.Int8OID, []byte("9007199254740993"), json.Number("9007199254740993")},
		{"numeric scale preserved", pgtype.NumericOID, []byte("1.50"), json.Number("1.50")},
		{"numeric NaN falls back to text", pgtype.NumericOID, []byte("NaN"), "NaN"},
		{"float with exponent", pgtype.Float8OID, []byte("1e+30"), json.Number("1e+30")},
		{"float infinity falls back to text", pgtype.Float8OID, []byte("Infinity"), "Infinity"},
		{"text", pgtype.TextOID, []byte("hello"), "hello"},
		{"timestamp stays text", pgtype.TimestamptzOID, []byte("2026-07-07 00:00:00+00"), "2026-07-07 00:00:00+00"},
		{"empty string", pgtype.TextOID, []byte(""), ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, convertV2Value(tc.oid, tc.raw))
		})
	}
}

func TestConvertV2ResultRows(t *testing.T) {
	fields := []pgconn.FieldDescription{
		{Name: "id", DataTypeOID: pgtype.Int4OID},
		{Name: "name", DataTypeOID: pgtype.TextOID},
	}
	rows := [][][]byte{
		{[]byte("1"), []byte("alice")},
		{[]byte("2"), []byte("bob")},
		{[]byte("3"), nil},
	}

	t.Run("converted with names and types", func(t *testing.T) {
		converted := convertV2ResultRows(fields, rows, 10)
		assert.Equal(t, []interface{}{
			map[string]interface{}{"id": json.Number("1"), "name": "alice"},
			map[string]interface{}{"id": json.Number("2"), "name": "bob"},
			map[string]interface{}{"id": json.Number("3"), "name": nil},
		}, converted)
	})

	t.Run("capped", func(t *testing.T) {
		assert.Len(t, convertV2ResultRows(fields, rows, 2), 2)
	})
}

func TestExtractV2TerminatePID(t *testing.T) {
	cases := []struct {
		commandString string
		expected      string
	}{
		{"select pg_catalog.pg_terminate_backend(66)", "66"},
		{"select pg_terminate_backend(123456)", "123456"},
		{"select 1", ""},
	}

	for _, tc := range cases {
		t.Run(tc.commandString, func(t *testing.T) {
			assert.Equal(t, tc.expected, extractV2TerminatePID(tc.commandString))
		})
	}
}

func TestV2CloneID(t *testing.T) {
	assert.Equal(t, "v2-31", v2CloneID("31"))
}

func TestNoticeRecorder(t *testing.T) {
	recorder := newNoticeRecorder()
	conn := &pgconn.PgConn{}
	otherConn := &pgconn.PgConn{}

	t.Run("uncaptured connections are ignored", func(t *testing.T) {
		recorder.handle(conn, &pgconn.Notice{Severity: "NOTICE", Message: "dropped"})
		assert.Nil(t, recorder.stop(conn))
	})

	t.Run("captures between start and stop, per connection", func(t *testing.T) {
		recorder.start(conn)
		recorder.handle(conn, &pgconn.Notice{Severity: "NOTICE", Message: "relation created"})
		recorder.handle(otherConn, &pgconn.Notice{Severity: "NOTICE", Message: "other conn"})
		recorder.handle(conn, &pgconn.Notice{Severity: "WARNING", Message: "watch out"})

		assert.Equal(t, []string{"NOTICE:  relation created", "WARNING:  watch out"}, recorder.stop(conn))
		assert.Nil(t, recorder.stop(conn), "stop clears the sink")
		assert.Nil(t, recorder.stop(otherConn), "uncaptured connection collected nothing")
	})

	t.Run("empty capture returns empty slice", func(t *testing.T) {
		recorder.start(conn)
		assert.Equal(t, []string{}, recorder.stop(conn))
	})
}

func TestV2EnvelopeStripping(t *testing.T) {
	t.Run("plan envelope", func(t *testing.T) {
		commandString := v2PlanEnvelopePrefix + "select * from users where id = 1"
		sql := commandString[len(v2PlanEnvelopePrefix):]
		assert.Equal(t, "select * from users where id = 1", sql)
	})

	t.Run("explain envelope", func(t *testing.T) {
		commandString := v2ExplainEnvelopePrefix + "delete from users" + v2ExplainEnvelopeSuffix
		sql := commandString[len(v2ExplainEnvelopePrefix) : len(commandString)-len(v2ExplainEnvelopeSuffix)]
		assert.Equal(t, "delete from users", sql)
	})
}

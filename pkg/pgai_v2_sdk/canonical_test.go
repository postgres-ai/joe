/*
2026 © Postgres.ai
*/

package pgaiv2sdk

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCanonicalizeNumbers locks the DECIMAL number contract
// (trim_scale(numeric)::text — NOT float64/JCS). The expected strings were
// verified against the platform canonicalizer
// (public.joe_jcs_canonicalize) running on Postgres.
func TestCanonicalizeNumbers(t *testing.T) {
	cases := []struct {
		literal  string
		expected string
	}{
		{"0", "0"},
		{"0.00", "0"},
		{"-0", "0"},
		{"-0.0", "0"},
		{"-0.000e5", "0"},
		{"12.34", "12.34"},
		{"1.50", "1.5"},
		{"123.4500", "123.45"},
		{"20.000", "20"},
		{"200", "200"},
		{"100", "100"},
		{"1e2", "100"},
		{"1E+2", "100"},
		{"1e-2", "0.01"},
		{"-1.5e-4", "-0.00015"},
		{"1.5e3", "1500"},
		{"1e20", "100000000000000000000"},
		{"9007199254740993", "9007199254740993"}, // beyond float64 exactness
		{"12345678901234567890.000000000000000001", "12345678901234567890.000000000000000001"},
		{"0.000000000000000000000000000001", "0.000000000000000000000000000001"},
		{"-987654321.1230", "-987654321.123"},
	}

	for _, tc := range cases {
		t.Run(tc.literal, func(t *testing.T) {
			actual, err := Canonicalize(json.Number(tc.literal))
			require.NoError(t, err)
			assert.Equal(t, tc.expected, actual)
		})
	}
}

func TestCanonicalizeInvalidNumbers(t *testing.T) {
	for _, literal := range []string{"", "-", "1e", "abc", "1.2.3", ".5", "0x10"} {
		t.Run(literal, func(t *testing.T) {
			_, err := Canonicalize(json.Number(literal))
			assert.Error(t, err)
		})
	}
}

// TestCanonicalizeStrings locks the to_json(text) escaping semantics:
// minimal escapes, \u00xx lowercase for control chars < 0x20, DEL and
// non-ASCII raw, '/' unescaped. Verified against the platform canonicalizer.
func TestCanonicalizeStrings(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		expected string
	}{
		{"plain", "alice", `"alice"`},
		{"quote and backslash", `O'Brien "q" \ /`, `"O'Brien \"q\" \\ /"`},
		{"named control escapes", "tab\tnl\ncr\rbs\bff\f", `"tab\tnl\ncr\rbs\bff\f"`},
		{"low control chars", "a\x01\x02\x1fb", `"a\u0001\u0002\u001fb"`},
		{"DEL stays raw", "a\x7fb", "\"a\x7fb\""},
		{"non-ASCII raw", "café — Ω😀", `"café — Ω😀"`},
		{"html chars unescaped", "<a>&amp;</a>", `"<a>&amp;</a>"`},
		{"empty", "", `""`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := Canonicalize(tc.input)
			require.NoError(t, err)
			assert.Equal(t, tc.expected, actual)
		})
	}
}

func TestCanonicalizeStructures(t *testing.T) {
	cases := []struct {
		name     string
		input    interface{}
		expected string
	}{
		{"null", nil, "null"},
		{"true", true, "true"},
		{"false", false, "false"},
		{"int", 42, "42"},
		{"int64", int64(-7), "-7"},
		{"empty array", []interface{}{}, "[]"},
		{"empty object", map[string]interface{}{}, "{}"},
		{
			name:     "keys sorted by code point",
			input:    map[string]interface{}{"Zz": 1, "ZZ": 2, "a b": 3, "a": 4, "A": 5, "": 6},
			expected: `{"":6,"A":5,"ZZ":2,"Zz":1,"a":4,"a b":3}`,
		},
		{
			name: "nested",
			input: map[string]interface{}{
				"b": json.Number("1"),
				"a": []interface{}{true, nil, "x/y"},
				"A": map[string]interface{}{"ünï": "é"},
			},
			expected: `{"A":{"ünï":"é"},"a":[true,null,"x/y"],"b":1}`,
		},
		{
			name:     "raw JSON decoded and canonicalized",
			input:    json.RawMessage(`{ "b" : 0.50, "a" : [ 1E1 ] }`),
			expected: `{"a":[10],"b":0.5}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := Canonicalize(tc.input)
			require.NoError(t, err)
			assert.Equal(t, tc.expected, actual)
		})
	}
}

func TestCanonicalizeRejectsFloat64(t *testing.T) {
	_, err := Canonicalize(float64(1.5))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "float64")

	_, err = Canonicalize(map[string]interface{}{"cost": float64(0.1)})
	assert.Error(t, err)
}

// TestCanonicalResult locks the per-command field concatenation, including
// the absent-field -> ” vs JSON-null -> `null` distinction and verbatim
// plan_text. Expected values verified against public.joe_canonical_result.
func TestCanonicalResult(t *testing.T) {
	cases := []struct {
		name     string
		payload  string
		command  string
		expected string
	}{
		{
			name:     "exec empty payload: absent fields",
			payload:  `{}`,
			command:  "exec",
			expected: "\n\n",
		},
		{
			name:     "exec null row_count vs absent notices",
			payload:  `{"result_rows":[],"row_count":null}`,
			command:  "exec",
			expected: "[]\nnull\n",
		},
		{
			name:     "plan with null plan_json",
			payload:  `{"plan_json":null}`,
			command:  "plan",
			expected: "\nnull",
		},
		{
			name:     "plan_text verbatim with newline",
			payload:  `{"plan_text":"line1\nline2","plan_json":{"Plan":{}}}`,
			command:  "plan",
			expected: "line1\nline2\n{\"Plan\":{}}",
		},
		{
			name:     "terminate",
			payload:  `{"terminated":true,"pid":66}`,
			command:  "terminate",
			expected: "true\n66",
		},
		{
			name:     "hypo (object keys canonicalized + boolean)",
			payload:  `{"hypo_used":true,"hypo_plan":{"Plan":{"Total Cost":0.10,"Node Type":"Index Scan"}}}`,
			command:  "hypo",
			expected: "{\"Plan\":{\"Node Type\":\"Index Scan\",\"Total Cost\":0.1}}\ntrue",
		},
		{
			name:     "hypo with absent hypo_used",
			payload:  `{"hypo_plan":{"Plan":"Index Scan"}}`,
			command:  "hypo",
			expected: "{\"Plan\":\"Index Scan\"}\n",
		},
		{
			name:     "reset",
			payload:  `{"reset":true}`,
			command:  "reset",
			expected: "true",
		},
		{
			name:     "describe object snapshot",
			payload:  `{"snapshot":{"output":"col | type\n","describe":"\\d x"}}`,
			command:  "describe",
			expected: `{"describe":"\\d x","output":"col | type\n"}`,
		},
		{
			name:     "extra keys are ignored (signed base fields)",
			payload:  `{"command_id":"1","nonce":"n","status":"done","command":"reset","reset":true}`,
			command:  "reset",
			expected: "true",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload, err := DecodeJSON([]byte(tc.payload))
			require.NoError(t, err)

			actual, err := CanonicalResult(payload, tc.command)
			require.NoError(t, err)
			assert.Equal(t, tc.expected, actual)
		})
	}
}

func TestCanonicalResultUnknownCommand(t *testing.T) {
	_, err := CanonicalResult(map[string]interface{}{}, "drop")
	assert.Error(t, err)
}

func TestDecodeJSONPreservesNumbers(t *testing.T) {
	payload, err := DecodeJSON([]byte(`{"cost":0.00,"big":9007199254740993}`))
	require.NoError(t, err)

	assert.Equal(t, json.Number("0.00"), payload["cost"])
	assert.Equal(t, json.Number("9007199254740993"), payload["big"])

	_, err = DecodeJSON([]byte(`[1,2]`))
	assert.Error(t, err, "top-level arrays are not reply bodies")

	_, err = DecodeJSON([]byte(`{"a":1} trailing`))
	assert.Error(t, err)
}

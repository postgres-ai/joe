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

// Frozen cross-implementation constants — MUST match platform-all
// db/functions/test_joe_callback_signing_vector.sql (AC23 / M1a.7a) exactly.
const (
	vectorSecret = "joe_v2_signing_vector_secret_key"
	vectorNonce  = "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6"
	vectorDigest = "d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5"
)

// TestFrozenSigningVector reproduces test_joe_callback_signing_vector
// byte-for-byte: the canonical 5th field AND the final HMAC of all three
// fixtures (plan / exec / activity). If this test fails, every genuine reply
// PT401s in production — do not "fix" the expectations; fix the code.
func TestFrozenSigningVector(t *testing.T) {
	cases := []struct {
		name              string
		commandID         string
		command           string
		payload           string
		expectedCanonical string
		expectedHMAC      string
	}{
		{
			name:      "plan",
			commandID: "4711",
			command:   "plan",
			payload: `{"plan_text":"Seq Scan on users  (cost=0.00..12.34 rows=200 width=8)\n  Filter: (status = $1)",` +
				`"plan_json":{"Plan":{"Node Type":"Seq Scan","Relation Name":"users","Startup Cost":0.00,` +
				`"Total Cost":12.34,"Plan Rows":200,"Plan Width":8}}}`,
			expectedCanonical: "Seq Scan on users  (cost=0.00..12.34 rows=200 width=8)\n  Filter: (status = $1)\n" +
				`{"Plan":{"Node Type":"Seq Scan","Plan Rows":200,"Plan Width":8,"Relation Name":"users",` +
				`"Startup Cost":0,"Total Cost":12.34}}`,
			expectedHMAC: "v0=bed096b5a3186f587b39b7ca8c2c040d56e22185ee2e4e4e8a9c47af2ce832b5",
		},
		{
			name:      "exec",
			commandID: "4712",
			command:   "exec",
			payload: `{"result_rows":[{"id":1,"name":"alice"},{"id":2,"name":"bob"}],"row_count":2,` +
				`"notices":["NOTICE:  relation created"]}`,
			expectedCanonical: `[{"id":1,"name":"alice"},{"id":2,"name":"bob"}]` + "\n2\n" +
				`["NOTICE:  relation created"]`,
			expectedHMAC: "v0=bdfa5a4cd8c1fb6b677a8cea8cbbf72a5f52401ba2292184b9315b5035ff405e",
		},
		{
			name:      "activity",
			commandID: "4713",
			command:   "activity",
			payload: `{"snapshot":{"captured_at":"2026-07-07T00:00:00Z",` +
				`"backends":[{"pid":12345,"state":"active","query":"select 1"}]}}`,
			expectedCanonical: `{"backends":[{"pid":12345,"query":"select 1","state":"active"}],` +
				`"captured_at":"2026-07-07T00:00:00Z"}`,
			expectedHMAC: "v0=6c21d3b05199a9f2667401f5dc3782cf6e0aaeede0e5090979a5656b179933a8",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload, err := DecodeJSON([]byte(tc.payload))
			require.NoError(t, err)

			canonical, err := CanonicalResult(payload, tc.command)
			require.NoError(t, err)
			require.Equal(t, tc.expectedCanonical, canonical, "canonical drift (field 5)")

			hmac := SignReplyMessage(tc.commandID, vectorNonce, StatusDone, tc.command,
				canonical, "", vectorDigest, []byte(vectorSecret))
			assert.Equal(t, tc.expectedHMAC, hmac, "HMAC drift")
		})
	}
}

// TestSignedBodyRoundTrip proves the sign-from-body-bytes property: the
// signature SignedBody produces verifies against a canonicalization of the
// exact body bytes it returns (the platform's own verification recipe).
func TestSignedBodyRoundTrip(t *testing.T) {
	secret := []byte("test-secret")

	cases := []struct {
		name  string
		reply *Reply
	}{
		{
			name: "done plan with raw EXPLAIN JSON",
			reply: &Reply{
				CommandID: "42",
				Nonce:     "nonce-42",
				Status:    StatusDone,
				Command:   CommandPlan,
				Result: map[string]interface{}{
					"plan_text": "Result  (cost=0.00..0.01 rows=1 width=4)",
					"plan_json": json.RawMessage("[\n  {\n    \"Plan\": {\n      \"Total Cost\": 0.01\n    }\n  }\n]"),
				},
			},
		},
		{
			name: "done exec",
			reply: &Reply{
				CommandID: "43",
				Nonce:     "nonce-43",
				Status:    StatusDone,
				Command:   CommandExec,
				Result: map[string]interface{}{
					"result_rows": []interface{}{map[string]interface{}{"n": json.Number("1")}},
					"row_count":   json.Number("1"),
					"notices":     []interface{}{},
				},
			},
		},
		{
			name: "error reply",
			reply: &Reply{
				CommandID: "44",
				Nonce:     "nonce-44",
				Status:    StatusError,
				Command:   CommandExec,
				Error:     `ERROR:  syntax error at or near "selct"`,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, signature, err := tc.reply.SignedBody(secret)
			require.NoError(t, err)

			// Recompute the signature from the body alone, the way the
			// platform does.
			payload, err := DecodeJSON(body)
			require.NoError(t, err)

			canonical, err := CanonicalResult(payload, tc.reply.Command)
			require.NoError(t, err)

			errField, _ := payload["error"].(string)
			digestField, _ := payload["live_conformance_digest"].(string)
			expected := SignReplyMessage(
				payload["command_id"].(string),
				payload["nonce"].(string),
				payload["status"].(string),
				payload["command"].(string),
				canonical, errField, digestField, secret)

			assert.Equal(t, expected, signature)
		})
	}
}

func TestReplyBodyJSONShape(t *testing.T) {
	t.Run("done reply carries result keys, no error key", func(t *testing.T) {
		reply := NewDoneReply(&DispatchRequest{
			SchemaVersion: SchemaVersion,
			CommandID:     "7",
			Command:       CommandReset,
			SessionID:     "1",
			Nonce:         "n",
			ReplyURL:      "http://example.com/rpc/joe_command_reply",
		}, map[string]interface{}{"reset": true})

		body, err := reply.BodyJSON()
		require.NoError(t, err)

		payload, err := DecodeJSON(body)
		require.NoError(t, err)
		assert.Equal(t, map[string]interface{}{
			"command_id": "7",
			"nonce":      "n",
			"status":     "done",
			"command":    "reset",
			"reset":      true,
		}, payload)
	})

	t.Run("error reply carries error key, no result keys", func(t *testing.T) {
		reply := NewErrorReply(&DispatchRequest{
			SchemaVersion: SchemaVersion,
			CommandID:     "8",
			Command:       CommandExec,
			SessionID:     "1",
			Nonce:         "n",
			ReplyURL:      "http://example.com/rpc/joe_command_reply",
		}, "boom")
		reply.Result = map[string]interface{}{"result_rows": []interface{}{}}

		body, err := reply.BodyJSON()
		require.NoError(t, err)

		payload, err := DecodeJSON(body)
		require.NoError(t, err)
		assert.Equal(t, map[string]interface{}{
			"command_id": "8",
			"nonce":      "n",
			"status":     "error",
			"command":    "exec",
			"error":      "boom",
		}, payload)
	})

	t.Run("html and unicode survive the body encoding", func(t *testing.T) {
		reply := &Reply{
			CommandID: "9", Nonce: "n", Status: StatusDone, Command: CommandExec,
			Result: map[string]interface{}{
				"result_rows": []interface{}{map[string]interface{}{"v": "<a> & 你好 🚀 café"}},
				"row_count":   json.Number("1"),
				"notices":     []interface{}{},
			},
		}

		body, err := reply.BodyJSON()
		require.NoError(t, err)
		assert.Contains(t, string(body), "<a> & 你好 🚀 café")
	})
}

func TestParseDispatchRequest(t *testing.T) {
	valid := `{"schema_version":2,"command_id":"12","command":"plan",` +
		`"command_string":"explain (format json, costs on, buffers off, analyze off, timing off) select 1",` +
		`"session_id":"3","nonce":"abcd","reply_url":"http://api.example.com/rpc/joe_command_reply",` +
		`"reply_signature_recipe":{"alg":"hmac-sha256"},"conformance_digest_expected":""}`

	t.Run("valid", func(t *testing.T) {
		req, err := ParseDispatchRequest([]byte(valid))
		require.NoError(t, err)
		assert.Equal(t, "12", req.CommandID)
		assert.Equal(t, "plan", req.Command)
		assert.Equal(t, "3", req.SessionID)
	})

	invalid := []struct {
		name string
		body string
	}{
		{"wrong schema version", `{"schema_version":1,"command_id":"1","command":"plan","session_id":"1","nonce":"n","reply_url":"u"}`},
		{"unknown command", `{"schema_version":2,"command_id":"1","command":"drop","session_id":"1","nonce":"n","reply_url":"u"}`},
		{"missing nonce", `{"schema_version":2,"command_id":"1","command":"plan","session_id":"1","reply_url":"u"}`},
		{"missing reply_url", `{"schema_version":2,"command_id":"1","command":"plan","session_id":"1","nonce":"n"}`},
		{"missing command_id", `{"schema_version":2,"command":"plan","session_id":"1","nonce":"n","reply_url":"u"}`},
		{"missing session_id", `{"schema_version":2,"command_id":"1","command":"plan","nonce":"n","reply_url":"u"}`},
		{"hypo without args.query", `{"schema_version":2,"command_id":"1","command":"hypo","session_id":"1","nonce":"n","reply_url":"u"}`},
		{"not json", `nope`},
	}

	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseDispatchRequest([]byte(tc.body))
			assert.Error(t, err)
		})
	}

	t.Run("hypo with args.query", func(t *testing.T) {
		body := `{"schema_version":2,"command_id":"1","command":"hypo","command_string":"create index on t (a)",` +
			`"session_id":"1","nonce":"n","reply_url":"u","args":{"query":"select * from t where a = 1"}}`
		req, err := ParseDispatchRequest([]byte(body))
		require.NoError(t, err)
		assert.Equal(t, "select * from t where a = 1", req.Args["query"])
	})
}

/*
2026 © Postgres.ai
*/

package pgaiv2sdk

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/pkg/errors"
)

// Signature scheme constants (shared with the platform's inbound verifier
// v1.joe_command_reply and Joe's own outbound-request verifier).
const (
	// SignatureHeader carries the reply HMAC.
	SignatureHeader = "x-joe-signature"
	// signaturePrefix prefixes the hex HMAC in the header value.
	signaturePrefix = "v0="
	// messagePrefix prefixes the signed message.
	messagePrefix = "v0:"
	// signedFieldCount is the size of the fixed signed concatenation.
	signedFieldCount = 7
)

// Reply statuses accepted by the platform callback.
const (
	StatusDone  = "done"
	StatusError = "error"
)

// Reply is a Joe API v2 command reply: the JSON callback body POSTed to
// reply_url plus everything needed to sign it.
type Reply struct {
	CommandID string
	Nonce     string
	Status    string // StatusDone / StatusError
	Command   string
	Error     string // included (and signed) only when Status == StatusError
	// ConformanceDigest is the live_conformance_digest (field 7). Sent and
	// signed only when non-empty; '' until the conformance gate (M1a.4c)
	// starts requesting it.
	ConformanceDigest string
	// Result holds the per-command result fields (e.g. plan_text/plan_json).
	// Values must be JSON-representable without float64: string, bool,
	// json.Number, json.RawMessage, int, int64, nil, []interface{},
	// map[string]interface{}.
	Result map[string]interface{}
}

// NewDoneReply builds a successful reply for a dispatch request.
func NewDoneReply(req *DispatchRequest, result map[string]interface{}) *Reply {
	return &Reply{
		CommandID: req.CommandID,
		Nonce:     req.Nonce,
		Status:    StatusDone,
		Command:   req.Command,
		Result:    result,
	}
}

// NewErrorReply builds an error reply for a dispatch request.
func NewErrorReply(req *DispatchRequest, errText string) *Reply {
	return &Reply{
		CommandID: req.CommandID,
		Nonce:     req.Nonce,
		Status:    StatusError,
		Command:   req.Command,
		Error:     errText,
	}
}

// BodyJSON serializes the reply body: the signed base fields plus the
// per-command result keys — exactly the platform's strict per-command key
// allowlist. json.RawMessage values (e.g. the EXPLAIN JSON) are spliced
// without re-encoding, so their number literals survive byte-verbatim.
func (r *Reply) BodyJSON() ([]byte, error) {
	body := map[string]interface{}{
		"command_id": r.CommandID,
		"nonce":      r.Nonce,
		"status":     r.Status,
		"command":    r.Command,
	}

	if r.Status == StatusError {
		body["error"] = r.Error
	} else {
		for key, value := range r.Result {
			body[key] = value
		}
	}

	if r.ConformanceDigest != "" {
		body["live_conformance_digest"] = r.ConformanceDigest
	}

	buf := &bytes.Buffer{}
	encoder := json.NewEncoder(buf)
	encoder.SetEscapeHTML(false)

	if err := encoder.Encode(body); err != nil {
		return nil, errors.Wrap(err, "failed to encode the reply body")
	}

	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// SignedBody returns the reply body and its x-joe-signature header value.
//
// The signature is computed FROM THE BODY BYTES (decode -> canonicalize),
// not from the in-memory result: whatever the body says is what gets signed,
// which is exactly the platform's own verification path (parse the body as
// jsonb -> joe_canonical_result -> HMAC). Any divergence therefore reduces
// to the canonicalizer contract frozen by the signing vector.
func (r *Reply) SignedBody(secret []byte) (body []byte, signature string, err error) {
	body, err = r.BodyJSON()
	if err != nil {
		return nil, "", err
	}

	payload, err := DecodeJSON(body)
	if err != nil {
		return nil, "", errors.Wrap(err, "failed to re-decode the reply body")
	}

	canonical, err := CanonicalResult(payload, r.Command)
	if err != nil {
		return nil, "", err
	}

	errField, _ := payload["error"].(string)
	digestField, _ := payload["live_conformance_digest"].(string)

	signature = SignReplyMessage(r.CommandID, r.Nonce, r.Status, r.Command, canonical, errField, digestField, secret)

	return body, signature, nil
}

// SignReplyMessage computes the x-joe-signature header value over the LOCKED
// 7-field LF-joined message:
//
//	command_id \n nonce \n status \n command \n canonicalResult \n
//	coalesce(error,'') \n coalesce(live_conformance_digest,'')
//
// as 'v0=' || hex(hmac-sha256('v0:' || message, secret)).
func SignReplyMessage(commandID, nonce, status, command, canonicalResult, errText, conformanceDigest string,
	secret []byte) string {
	fields := make([]string, 0, signedFieldCount)
	fields = append(fields, commandID, nonce, status, command, canonicalResult, errText, conformanceDigest)

	message := strings.Join(fields, "\n")

	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(messagePrefix)) // nolint: errcheck
	mac.Write([]byte(message))       // nolint: errcheck

	return signaturePrefix + hex.EncodeToString(mac.Sum(nil))
}

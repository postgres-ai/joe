/*
2026 © Postgres.ai
*/

package pgaiv2sdk

import (
	"encoding/json"
	"net/url"
	"strings"

	"github.com/pkg/errors"
)

// SchemaVersion is the Joe API v2 dispatch schema version.
const SchemaVersion = 2

// DispatchRequest is the platform's versioned outbound dispatch envelope
// (public.joe_command_dispatch) POSTed to Joe's /webui/command endpoint with
// schema_version 2. The request body is authenticated by the existing
// Verification-Signature HMAC before it reaches this parser.
type DispatchRequest struct {
	SchemaVersion int    `json:"schema_version"`
	CommandID     string `json:"command_id"`
	Command       string `json:"command"`
	CommandString string `json:"command_string"`
	SessionID     string `json:"session_id"`
	Nonce         string `json:"nonce"`
	ReplyURL      string `json:"reply_url"`
	// ReplySignatureRecipe describes the reply signature for humans/debugging;
	// the recipe is LOCKED by contract, so it is carried opaquely.
	ReplySignatureRecipe json.RawMessage `json:"reply_signature_recipe"`
	// ConformanceDigestExpected is '' until the conformance gate (M1a.4c)
	// starts requesting a live digest echo.
	ConformanceDigestExpected string `json:"conformance_digest_expected"`
	// Args carries typed per-command arguments. Today only hypo uses it:
	// args.query is the target query the hypothetical index is evaluated
	// against (never interpolated into command_string).
	Args map[string]string `json:"args"`
}

// ParseDispatchRequest decodes and validates a v2 dispatch request body.
func ParseDispatchRequest(body []byte) (*DispatchRequest, error) {
	request := &DispatchRequest{}
	if err := json.Unmarshal(body, request); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal the dispatch request")
	}

	if err := request.Validate(); err != nil {
		return nil, err
	}

	return request, nil
}

// Validate enforces the dispatch envelope invariants.
func (r *DispatchRequest) Validate() error {
	if r.SchemaVersion != SchemaVersion {
		return errors.Errorf("unsupported schema_version %d", r.SchemaVersion)
	}

	if r.CommandID == "" {
		return errors.New("command_id must not be empty")
	}

	if !IsSupportedCommand(r.Command) {
		return errors.Errorf("unsupported command %q", r.Command)
	}

	if r.SessionID == "" {
		return errors.New("session_id must not be empty")
	}

	if r.Nonce == "" {
		return errors.New("nonce must not be empty")
	}

	if r.ReplyURL == "" {
		return errors.New("reply_url must not be empty")
	}

	if err := validateReplyURL(r.ReplyURL); err != nil {
		return errors.Wrap(err, "invalid reply_url")
	}

	if r.Command == CommandHypo && r.Args["query"] == "" {
		return errors.New("hypo requires args.query")
	}

	return nil
}

// validateReplyURL enforces the https-only reply callback scheme (SSRF
// hardening): the signed reply carries query results and plans, so it must
// never be POSTed over plaintext, to a non-HTTP scheme, or to a host-less
// (opaque/relative) URL.
func validateReplyURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return err
	}

	if parsed.Scheme != "https" {
		return errors.Errorf("scheme %q is not allowed (https only)", parsed.Scheme)
	}

	if parsed.Hostname() == "" {
		return errors.New("host must not be empty")
	}

	return nil
}

// ValidateReplyURLHost verifies the reply_url points at one of the allowed
// callback hosts (SSRF allowlist, pinned to the platform callback host). The
// comparison is a case-insensitive hostname match; an empty allowlist fails
// closed.
func ValidateReplyURLHost(rawURL string, allowedHosts []string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return errors.Wrap(err, "invalid reply_url")
	}

	host := strings.ToLower(parsed.Hostname())

	for _, allowed := range allowedHosts {
		if allowed != "" && strings.ToLower(allowed) == host {
			return nil
		}
	}

	return errors.Errorf("reply_url host %q is not in the callback host allowlist", parsed.Hostname())
}

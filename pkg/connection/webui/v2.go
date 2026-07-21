/*
2026 © Postgres.ai
*/

package webui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime/debug"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/pkg/errors"

	"gitlab.com/postgres-ai/database-lab/v3/pkg/log"

	pgaiv2sdk "gitlab.com/postgres-ai/joe/pkg/pgai_v2_sdk"
)

// Joe API v2 processing constants.
const (
	// v2ProcessTimeout is the overall leak guard around one dispatch. The
	// real budgets live in msgproc (lock wait + execution, each 10 min,
	// with the execution budget starting only after the session lock —
	// L1); this outer bound only catches bugs, so it must exceed their
	// sum.
	v2ProcessTimeout = 21 * time.Minute

	// v2ReplyGrace delays the reply POST slightly: the dispatch POST happens
	// inside the platform's consume transaction, and the reply row lock
	// serializes against that commit — the grace avoids needless lock waits.
	v2ReplyGrace = 200 * time.Millisecond

	// v2ReplyAttempts bounds reply delivery attempts (network errors and
	// 5xx responses are retried; 4xx are deterministic and are not).
	v2ReplyAttempts = 3

	// v2ReplyRetryBackoff is the pause between reply delivery attempts.
	v2ReplyRetryBackoff = 2 * time.Second

	// v2ErrorMaxBytes caps the error text included in an error reply.
	v2ErrorMaxBytes = 500

	// v2ReplyTimeout bounds a single reply POST.
	v2ReplyTimeout = 60 * time.Second

	// v2ReplyDeliveryTimeout bounds the WHOLE reply delivery (grace + every
	// attempt + backoff). The reply runs on its own budget, independent of
	// the execution context: a command that consumed its full execution
	// timeout must not be starved of its (cheap) reply-delivery window.
	v2ReplyDeliveryTimeout = v2ReplyGrace + v2ReplyAttempts*(v2ReplyTimeout+v2ReplyRetryBackoff)

	// v2SeenCommandTTL bounds the replay-protection window (M3): a
	// command_id observed within the TTL is acked but not re-executed. The
	// window comfortably exceeds the platform's per-command lifecycle.
	v2SeenCommandTTL = 30 * time.Minute

	// v2SeenCommandCap hard-bounds the dedup cache size; at the cap, new
	// command IDs are rejected (fail closed — the platform retries later)
	// rather than silently dropping replay protection.
	v2SeenCommandCap = 100000
)

// v2CommandDeduper is a bounded-TTL seen-command_id cache (M3 replay
// protection). The zero value is ready to use.
type v2CommandDeduper struct {
	mu   sync.Mutex
	seen map[string]time.Time
}

// markSeen records the command_id and reports whether the dispatch must be
// treated as a duplicate (already seen within the TTL, or the cache is at
// its hard cap).
func (d *v2CommandDeduper) markSeen(commandID string, now time.Time) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.seen == nil {
		d.seen = make(map[string]time.Time)
	}

	for id, seenAt := range d.seen {
		if now.Sub(seenAt) > v2SeenCommandTTL {
			delete(d.seen, id)
		}
	}

	if _, ok := d.seen[commandID]; ok {
		return true
	}

	if len(d.seen) >= v2SeenCommandCap {
		log.Err("v2: the seen-command cache is full; treating the dispatch as a duplicate (fail closed)")
		return true
	}

	d.seen[commandID] = now

	return false
}

// v2CommandExecutor runs a Joe API v2 command on a Database Lab clone.
// Implemented by *msgproc.ProcessingService.
type v2CommandExecutor interface {
	ExecuteV2Command(ctx context.Context, req *pgaiv2sdk.DispatchRequest) (map[string]interface{}, error)
}

// v2SchemaProbe sniffs the dispatch schema version out of a command body.
type v2SchemaProbe struct {
	SchemaVersion int `json:"schema_version"`
}

// isV2Dispatch reports whether the (already HMAC-verified) command body is a
// Joe API v2 dispatch request.
func isV2Dispatch(body []byte) bool {
	probe := v2SchemaProbe{}
	if err := json.Unmarshal(body, &probe); err != nil {
		return false
	}

	return probe.SchemaVersion == pgaiv2sdk.SchemaVersion
}

// handleV2Command accepts a v2 dispatch request: it validates the envelope,
// acks synchronously, and executes + replies asynchronously (the dispatch
// POST happens inside the platform's consume transaction, so the reply MUST
// NOT be produced in-band).
func (a *Assistant) handleV2Command(w http.ResponseWriter, body []byte) {
	if !a.appCfg.APIV2.Enabled {
		log.Err("Joe API v2 dispatch received, but apiV2.enabled is false; rejecting")
		w.WriteHeader(http.StatusBadRequest)

		return
	}

	request, err := pgaiv2sdk.ParseDispatchRequest(body)
	if err != nil {
		log.Err("Failed to parse the v2 dispatch request:", err)
		w.WriteHeader(http.StatusBadRequest)

		return
	}

	// SSRF hardening: the signed reply (query results, plans) may only be
	// POSTed to the pinned platform callback host.
	if err := pgaiv2sdk.ValidateReplyURLHost(request.ReplyURL, a.v2AllowedReplyHosts()); err != nil {
		log.Err("Rejected the v2 dispatch reply_url:", err)
		w.WriteHeader(http.StatusBadRequest)

		return
	}

	executor, err := a.getV2Executor()
	if err != nil {
		log.Err("Failed to resolve a v2 command executor:", err)
		w.WriteHeader(http.StatusBadRequest)

		return
	}

	// Replay protection (M3): a command_id seen within the TTL is acked
	// (the platform already owns this command's lifecycle) but NOT
	// re-executed — replays of exec/terminate/reset must have no effect.
	if a.v2SeenCommands.markSeen(request.CommandID, time.Now()) {
		log.Msg("v2: duplicate dispatch ignored, command_id:", request.CommandID)
		writeV2Ack(w)

		return
	}

	go a.processV2Command(executor, request)

	writeV2Ack(w)
}

// writeV2Ack writes the synchronous dispatch acknowledgement.
func writeV2Ack(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	if err := json.NewEncoder(w).Encode(map[string]bool{"accepted": true}); err != nil {
		log.Err("Failed to encode the v2 ack:", err)
	}
}

// getV2Executor resolves the processing service executing v2 commands. The
// v2 dispatch envelope carries no channel, so the first configured webui
// channel's processor (and thus its Database Lab instance) is used — the
// same convention as the channelsHandler.
func (a *Assistant) getV2Executor() (v2CommandExecutor, error) {
	workspaces, ok := a.appCfg.ChannelMapping.CommunicationTypes[CommunicationType]
	if !ok || len(workspaces) == 0 || len(workspaces[0].Channels) == 0 {
		return nil, errors.New("no webui channels configured")
	}

	channelID := workspaces[0].Channels[0].ChannelID

	svc, err := a.getProcessingService(channelID)
	if err != nil {
		return nil, err
	}

	executor, ok := svc.(v2CommandExecutor)
	if !ok {
		return nil, errors.Errorf("message processor for channel %q cannot execute v2 commands", channelID)
	}

	return executor, nil
}

// processV2Command executes the command and delivers the signed reply.
func (a *Assistant) processV2Command(executor v2CommandExecutor, request *pgaiv2sdk.DispatchRequest) {
	ctx, cancel := context.WithTimeout(context.Background(), v2ProcessTimeout)
	defer cancel()

	log.Dbg(fmt.Sprintf("v2: processing command_id=%s command=%s session=%s",
		request.CommandID, request.Command, request.SessionID))

	var reply *pgaiv2sdk.Reply

	result, err := a.executeV2Command(ctx, executor, request)
	if err != nil {
		log.Err(fmt.Sprintf("v2: command_id=%s failed: %v", request.CommandID, err))

		reply = pgaiv2sdk.NewErrorReply(request, truncateV2Error(err.Error()))
	} else {
		reply = pgaiv2sdk.NewDoneReply(request, result)
	}

	// Deliver the reply on a fresh context: the execution ctx may be
	// (nearly) exhausted by a long command, and a late signed reply is
	// still accepted by the platform (timed_out -> done).
	replyCtx, cancelReply := context.WithTimeout(context.Background(), v2ReplyDeliveryTimeout)
	defer cancelReply()

	if err := a.postV2Reply(replyCtx, request.ReplyURL, reply); err != nil {
		log.Err(fmt.Sprintf("v2: command_id=%s reply delivery failed: %v", request.CommandID, err))
		return
	}

	log.Dbg(fmt.Sprintf("v2: command_id=%s replied status=%s", request.CommandID, reply.Status))
}

// executeV2Command runs the executor with panic recovery (H3): a panic in
// command execution must fail THIS command with a generic error reply — it
// runs in a bare goroutine, so without recovery it would kill the whole Joe
// process and every in-flight session. The panic value stays in the log; it
// must not leak into the platform-facing reply.
func (a *Assistant) executeV2Command(ctx context.Context, executor v2CommandExecutor,
	request *pgaiv2sdk.DispatchRequest) (result map[string]interface{}, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Err(fmt.Sprintf("v2: panic while executing command_id=%s: %v\n%s",
				request.CommandID, recovered, debug.Stack()))

			result, err = nil, errors.New("internal error while executing the command")
		}
	}()

	return executor.ExecuteV2Command(ctx, request)
}

// postV2Reply signs the reply per the locked contract and POSTs it to the
// dispatch's reply_url with the x-joe-signature header.
func (a *Assistant) postV2Reply(ctx context.Context, replyURL string, reply *pgaiv2sdk.Reply) error {
	body, signature, err := reply.SignedBody(a.v2ReplySecret())
	if err != nil {
		return errors.Wrap(err, "failed to sign the reply")
	}

	// L2: every wait selects against ctx so an expired delivery budget
	// stops the loop instead of sleeping into doomed POSTs.
	if err := v2Sleep(ctx, v2ReplyGrace); err != nil {
		return err
	}

	var lastErr error

	for attempt := 1; attempt <= v2ReplyAttempts; attempt++ {
		if attempt > 1 {
			if err := v2Sleep(ctx, v2ReplyRetryBackoff); err != nil {
				return lastErr
			}
		}

		retriable, err := a.deliverV2Reply(ctx, replyURL, body, signature)
		if err == nil {
			return nil
		}

		lastErr = err

		if !retriable {
			return lastErr
		}

		log.Dbg(fmt.Sprintf("v2: reply attempt %d/%d failed: %v", attempt, v2ReplyAttempts, err))
	}

	return lastErr
}

// deliverV2Reply performs one reply POST. It reports whether a failure is
// worth retrying (network errors and 5xx responses are; 4xx rejections are
// deterministic and are not).
func (a *Assistant) deliverV2Reply(ctx context.Context, replyURL string, body []byte, signature string) (bool, error) {
	postCtx, cancel := context.WithTimeout(ctx, v2ReplyTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(postCtx, http.MethodPost, replyURL, bytes.NewReader(body))
	if err != nil {
		return false, errors.Wrap(err, "failed to build the reply request")
	}

	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(pgaiv2sdk.SignatureHeader, signature)

	response, err := a.v2ReplyHTTPClient().Do(request)
	if err != nil {
		// A dead delivery budget is not retriable (L2) — only transient
		// network failures are.
		if ctx.Err() != nil {
			return false, errors.Wrap(err, "reply delivery budget exhausted")
		}

		return true, errors.Wrap(err, "reply POST failed")
	}

	defer func() {
		if err := response.Body.Close(); err != nil {
			log.Dbg("failed to close the reply response body:", err)
		}
	}()

	if response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
		return false, nil
	}

	preview, _ := io.ReadAll(io.LimitReader(response.Body, v2ErrorMaxBytes))

	return response.StatusCode >= http.StatusInternalServerError,
		errors.Errorf("reply POST returned %d: %s", response.StatusCode, string(preview))
}

// v2AllowedReplyHosts resolves the reply callback host allowlist: the
// dedicated apiV2.replyHost when configured, else the hostname of the
// platform API URL. An empty result fails closed in ValidateReplyURLHost.
func (a *Assistant) v2AllowedReplyHosts() []string {
	if host := a.appCfg.APIV2.ReplyHost; host != "" {
		return []string{host}
	}

	platformURL, err := url.Parse(a.appCfg.Platform.URL)
	if err != nil || platformURL.Hostname() == "" {
		return nil
	}

	return []string{platformURL.Hostname()}
}

// v2ReplyHTTPClient builds the hardened client for reply delivery: it never
// follows redirects, so a validated callback host cannot bounce the signed
// reply to another address (SSRF). The transport is injectable for tests.
func (a *Assistant) v2ReplyHTTPClient() *http.Client {
	transport := a.v2ReplyTransport
	if transport == nil {
		transport = http.DefaultTransport
	}

	return &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, _ []*http.Request) error {
			return errors.Errorf("v2 reply redirect to %q refused", req.URL.Redacted())
		},
	}
}

// v2ReplySecret resolves the reply signing secret: the dedicated replySecret
// when configured, else the workspace signing secret (the instance verify
// token — the secret the platform verifies replies with).
func (a *Assistant) v2ReplySecret() []byte {
	if a.appCfg.APIV2.ReplySecret != "" {
		return []byte(a.appCfg.APIV2.ReplySecret)
	}

	return []byte(a.credentialsCfg.SigningSecret)
}

// v2Sleep waits for the duration or until ctx is done, whichever is first.
func v2Sleep(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// truncateV2Error caps the error text at v2ErrorMaxBytes without splitting a
// UTF-8 sequence.
func truncateV2Error(errText string) string {
	if len(errText) <= v2ErrorMaxBytes {
		return errText
	}

	truncated := errText[:v2ErrorMaxBytes]

	for len(truncated) > 0 && !utf8.ValidString(truncated) {
		truncated = truncated[:len(truncated)-1]
	}

	return truncated
}

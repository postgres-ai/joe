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
	"time"
	"unicode/utf8"

	"github.com/pkg/errors"

	"gitlab.com/postgres-ai/database-lab/v3/pkg/log"

	pgaiv2sdk "gitlab.com/postgres-ai/joe/pkg/pgai_v2_sdk"
)

// Joe API v2 processing constants.
const (
	// v2ExecutionTimeout caps a single command execution. The platform
	// sweeps its own (shorter) per-command timeout; a late reply is still
	// accepted (timed_out -> done), so this is only a leak guard.
	v2ExecutionTimeout = 10 * time.Minute

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
)

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

	executor, err := a.getV2Executor()
	if err != nil {
		log.Err("Failed to resolve a v2 command executor:", err)
		w.WriteHeader(http.StatusBadRequest)

		return
	}

	go a.processV2Command(executor, request)

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
	ctx, cancel := context.WithTimeout(context.Background(), v2ExecutionTimeout)
	defer cancel()

	log.Dbg(fmt.Sprintf("v2: processing command_id=%s command=%s session=%s",
		request.CommandID, request.Command, request.SessionID))

	var reply *pgaiv2sdk.Reply

	result, err := executor.ExecuteV2Command(ctx, request)
	if err != nil {
		log.Err(fmt.Sprintf("v2: command_id=%s failed: %v", request.CommandID, err))

		reply = pgaiv2sdk.NewErrorReply(request, truncateV2Error(err.Error()))
	} else {
		reply = pgaiv2sdk.NewDoneReply(request, result)
	}

	if err := a.postV2Reply(ctx, request.ReplyURL, reply); err != nil {
		log.Err(fmt.Sprintf("v2: command_id=%s reply delivery failed: %v", request.CommandID, err))
		return
	}

	log.Dbg(fmt.Sprintf("v2: command_id=%s replied status=%s", request.CommandID, reply.Status))
}

// postV2Reply signs the reply per the locked contract and POSTs it to the
// dispatch's reply_url with the x-joe-signature header.
func (a *Assistant) postV2Reply(ctx context.Context, replyURL string, reply *pgaiv2sdk.Reply) error {
	body, signature, err := reply.SignedBody(a.v2ReplySecret())
	if err != nil {
		return errors.Wrap(err, "failed to sign the reply")
	}

	time.Sleep(v2ReplyGrace)

	var lastErr error

	for attempt := 1; attempt <= v2ReplyAttempts; attempt++ {
		if attempt > 1 {
			time.Sleep(v2ReplyRetryBackoff)
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

	response, err := http.DefaultClient.Do(request)
	if err != nil {
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

// v2ReplySecret resolves the reply signing secret: the dedicated replySecret
// when configured, else the workspace signing secret (the instance verify
// token — the secret the platform verifies replies with).
func (a *Assistant) v2ReplySecret() []byte {
	if a.appCfg.APIV2.ReplySecret != "" {
		return []byte(a.appCfg.APIV2.ReplySecret)
	}

	return []byte(a.credentialsCfg.SigningSecret)
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

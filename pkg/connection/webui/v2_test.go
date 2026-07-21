/*
2026 © Postgres.ai
*/

package webui

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"gitlab.com/postgres-ai/joe/pkg/config"
	"gitlab.com/postgres-ai/joe/pkg/connection"
	"gitlab.com/postgres-ai/joe/pkg/models"
	pgaiv2sdk "gitlab.com/postgres-ai/joe/pkg/pgai_v2_sdk"
	"gitlab.com/postgres-ai/joe/pkg/services/usermanager"
)

func TestIsV2Dispatch(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		expected bool
	}{
		{
			name:     "v2 dispatch",
			body:     `{"schema_version":2,"command_id":"1","command":"plan"}`,
			expected: true,
		},
		{
			name:     "v1 webui message",
			body:     `{"session_id":"s","command_id":"c","text":"explain select 1","channel_id":"ch","user_id":"u"}`,
			expected: false,
		},
		{"wrong version", `{"schema_version":1}`, false},
		{"not json", `nope`, false},
		{"empty", ``, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, isV2Dispatch([]byte(tc.body)))
		})
	}
}

func TestTruncateV2Error(t *testing.T) {
	t.Run("short text unchanged", func(t *testing.T) {
		assert.Equal(t, "boom", truncateV2Error("boom"))
	})

	t.Run("long text capped", func(t *testing.T) {
		long := strings.Repeat("x", 2*v2ErrorMaxBytes)
		assert.Len(t, truncateV2Error(long), v2ErrorMaxBytes)
	})

	t.Run("multibyte rune not split", func(t *testing.T) {
		long := strings.Repeat("x", v2ErrorMaxBytes-1) + "🚀"
		truncated := truncateV2Error(long)
		assert.LessOrEqual(t, len(truncated), v2ErrorMaxBytes)
		assert.True(t, strings.HasSuffix(truncated, "x"))
	})
}

func TestDeliverV2Reply(t *testing.T) {
	assistant := &Assistant{}
	ctx := context.Background()

	t.Run("2xx delivered, not retriable", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		retriable, err := assistant.deliverV2Reply(ctx, server.URL, []byte(`{}`), "v0=sig")
		assert.NoError(t, err)
		assert.False(t, retriable)
	})

	t.Run("4xx deterministic, not retriable", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "bad signature", http.StatusBadRequest)
		}))
		defer server.Close()

		retriable, err := assistant.deliverV2Reply(ctx, server.URL, []byte(`{}`), "v0=sig")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "400")
		assert.False(t, retriable)
	})

	t.Run("5xx retriable", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "temporary", http.StatusInternalServerError)
		}))
		defer server.Close()

		retriable, err := assistant.deliverV2Reply(ctx, server.URL, []byte(`{}`), "v0=sig")
		assert.Error(t, err)
		assert.True(t, retriable)
	})

	t.Run("redirects refused, target never contacted", func(t *testing.T) {
		// SSRF hardening (SB1): a validated reply host must not be able to
		// bounce the signed reply (with query results) to another address.
		var targetHits atomic.Int32

		target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			targetHits.Add(1)
			w.WriteHeader(http.StatusOK)
		}))
		defer target.Close()

		redirector := httptest.NewServer(http.RedirectHandler(target.URL, http.StatusFound))
		defer redirector.Close()

		_, err := assistant.deliverV2Reply(ctx, redirector.URL, []byte(`{}`), "v0=sig")
		assert.Error(t, err)
		assert.EqualValues(t, 0, targetHits.Load(), "the redirect target must never receive the reply")
	})

	t.Run("connection refused retriable", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		deadURL := server.URL

		server.Close()

		retriable, err := assistant.deliverV2Reply(ctx, deadURL, []byte(`{}`), "v0=sig")
		assert.Error(t, err)
		assert.True(t, retriable)
	})
}

func TestPostV2Reply(t *testing.T) {
	assistant := &Assistant{
		credentialsCfg: &config.Credentials{SigningSecret: "test-secret"},
		appCfg:         &config.Config{},
	}
	reply := pgaiv2sdk.NewDoneReply(&pgaiv2sdk.DispatchRequest{
		CommandID: "17",
		Nonce:     "nonce",
		Command:   pgaiv2sdk.CommandReset,
	}, map[string]interface{}{"reset": true})

	t.Run("delivered on the first attempt", func(t *testing.T) {
		var hits atomic.Int32

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
			assert.True(t, strings.HasPrefix(r.Header.Get(pgaiv2sdk.SignatureHeader), "v0="))
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		assert.NoError(t, assistant.postV2Reply(context.Background(), server.URL, reply))
		assert.EqualValues(t, 1, hits.Load())
	})

	t.Run("4xx not retried, error surfaced", func(t *testing.T) {
		var hits atomic.Int32

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			hits.Add(1)
			http.Error(w, "bad reply", http.StatusBadRequest)
		}))
		defer server.Close()

		err := assistant.postV2Reply(context.Background(), server.URL, reply)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "400")
		assert.EqualValues(t, 1, hits.Load())
	})

	t.Run("5xx retried exactly v2ReplyAttempts times", func(t *testing.T) {
		var hits atomic.Int32

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			hits.Add(1)
			http.Error(w, "temporary", http.StatusInternalServerError)
		}))
		defer server.Close()

		err := assistant.postV2Reply(context.Background(), server.URL, reply)
		assert.Error(t, err)
		assert.EqualValues(t, v2ReplyAttempts, hits.Load())
	})
}

// fakeV2Executor is a connection.MessageProcessor that records v2 dispatches.
type fakeV2Executor struct {
	requests chan *pgaiv2sdk.DispatchRequest
	result   map[string]interface{}
}

func (f *fakeV2Executor) ProcessMessageEvent(context.Context, models.IncomingMessage) {}
func (f *fakeV2Executor) ProcessAppMentionEvent(models.IncomingMessage)               {}
func (f *fakeV2Executor) RestoreSessions(context.Context) error                       { return nil }
func (f *fakeV2Executor) CheckIdleSessions(context.Context)                           {}
func (f *fakeV2Executor) Users() usermanager.UserList                                 { return nil }

func (f *fakeV2Executor) ExecuteV2Command(_ context.Context,
	req *pgaiv2sdk.DispatchRequest) (map[string]interface{}, error) {
	if f.requests != nil {
		f.requests <- req
	}

	return f.result, nil
}

func TestHandleV2Command(t *testing.T) {
	const (
		signingSecret = "test-secret"
		channelID     = "ProductionDB"
	)

	signBody := func(body []byte) string {
		mac := hmac.New(sha256.New, []byte(signingSecret))
		mac.Write([]byte(bodyPrefix))
		mac.Write(body)

		return signaturePrefix + hex.EncodeToString(mac.Sum(nil))
	}

	dispatchBody := func(replyURL string) []byte {
		body, err := json.Marshal(map[string]interface{}{
			"schema_version": pgaiv2sdk.SchemaVersion,
			"command_id":     "42",
			"command":        pgaiv2sdk.CommandReset,
			"command_string": "reset",
			"session_id":     "31",
			"nonce":          "test-nonce",
			"reply_url":      replyURL,
		})
		assert.NoError(t, err)

		return body
	}

	newAssistant := func(enabled bool, executor connection.MessageProcessor) *Assistant {
		return &Assistant{
			credentialsCfg: &config.Credentials{SigningSecret: signingSecret},
			appCfg: &config.Config{
				APIV2: config.APIV2{Enabled: enabled},
				ChannelMapping: &config.ChannelMapping{
					CommunicationTypes: map[string][]config.Workspace{
						CommunicationType: {{Channels: []config.Channel{{ChannelID: channelID}}}},
					},
				},
			},
			msgProcessors: map[string]connection.MessageProcessor{channelID: executor},
		}
	}

	// post routes the body through the inbound HMAC verifier and the shared
	// command endpoint — the same path a platform dispatch takes.
	post := func(assistant *Assistant, body []byte, signature string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/webui/command", strings.NewReader(string(body)))
		request.Header.Set(VerificationSignatureKey, signature)

		recorder := httptest.NewRecorder()
		NewVerifier([]byte(signingSecret)).Handler(assistant.commandHandler)(recorder, request)

		return recorder
	}

	t.Run("apiV2 disabled rejected with 400", func(t *testing.T) {
		body := dispatchBody("http://reply.invalid")
		recorder := post(newAssistant(false, &fakeV2Executor{}), body, signBody(body))
		assert.Equal(t, http.StatusBadRequest, recorder.Code)
	})

	t.Run("invalid inbound HMAC rejected before dispatch", func(t *testing.T) {
		body := dispatchBody("http://reply.invalid")
		recorder := post(newAssistant(true, &fakeV2Executor{}), body, "v0=deadbeef")
		assert.Equal(t, http.StatusForbidden, recorder.Code)
	})

	t.Run("reply_url host outside the allowlist rejected with 400", func(t *testing.T) {
		// SSRF hardening (SB1): the reply host is pinned to the platform
		// callback host — an HMAC-valid dispatch must not be able to point
		// the signed reply (query results) at an arbitrary address.
		executor := &fakeV2Executor{requests: make(chan *pgaiv2sdk.DispatchRequest, 1)}
		assistant := newAssistant(true, executor)
		assistant.appCfg.Platform.URL = "https://platform.example.com/api/general"

		body := dispatchBody("https://evil.example.com/rpc/joe_command_reply")
		recorder := post(assistant, body, signBody(body))
		assert.Equal(t, http.StatusBadRequest, recorder.Code)

		select {
		case <-executor.requests:
			t.Fatal("a dispatch with a non-allowlisted reply_url must not execute")
		case <-time.After(100 * time.Millisecond):
		}
	})

	t.Run("invalid envelope rejected with 400", func(t *testing.T) {
		body := []byte(`{"schema_version":2,"command":"reset"}`)
		recorder := post(newAssistant(true, &fakeV2Executor{}), body, signBody(body))
		assert.Equal(t, http.StatusBadRequest, recorder.Code)
	})

	t.Run("replayed dispatch acked but not re-executed", func(t *testing.T) {
		// M3: a captured HMAC-valid dispatch replayed to the endpoint must
		// not re-execute side-effecting commands (exec/terminate/reset).
		executor := &fakeV2Executor{
			requests: make(chan *pgaiv2sdk.DispatchRequest, 2),
			result:   map[string]interface{}{"reset": true},
		}
		assistant := newAssistant(true, executor)
		assistant.appCfg.APIV2.ReplyHost = "reply.example.com"

		body := dispatchBody("https://reply.example.com/rpc/joe_command_reply")

		first := post(assistant, body, signBody(body))
		assert.Equal(t, http.StatusOK, first.Code)

		select {
		case <-executor.requests:
		case <-time.After(5 * time.Second):
			t.Fatal("the first dispatch was not executed")
		}

		replay := post(assistant, body, signBody(body))
		assert.Equal(t, http.StatusOK, replay.Code, "a replay is acked (the platform already owns the command state)")

		select {
		case <-executor.requests:
			t.Fatal("a replayed command_id must not re-execute")
		case <-time.After(200 * time.Millisecond):
		}
	})

	t.Run("valid dispatch acked and signed reply delivered", func(t *testing.T) {
		type capturedReply struct {
			signature string
			body      []byte
		}

		replies := make(chan capturedReply, 1)

		// TLS + the transport seam: reply_url validation demands https and
		// an allowlisted host, exactly like production.
		replyServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			replyBody, err := io.ReadAll(r.Body)
			assert.NoError(t, err)

			replies <- capturedReply{signature: r.Header.Get(pgaiv2sdk.SignatureHeader), body: replyBody}

			w.WriteHeader(http.StatusOK)
		}))
		defer replyServer.Close()

		executor := &fakeV2Executor{
			requests: make(chan *pgaiv2sdk.DispatchRequest, 1),
			result:   map[string]interface{}{"reset": true},
		}

		assistant := newAssistant(true, executor)
		assistant.appCfg.APIV2.ReplyHost = "127.0.0.1"
		assistant.v2ReplyTransport = replyServer.Client().Transport

		body := dispatchBody(replyServer.URL)
		recorder := post(assistant, body, signBody(body))

		assert.Equal(t, http.StatusOK, recorder.Code)
		assert.JSONEq(t, `{"accepted": true}`, recorder.Body.String())

		select {
		case req := <-executor.requests:
			assert.Equal(t, "42", req.CommandID)
			assert.Equal(t, "31", req.SessionID)
		case <-time.After(5 * time.Second):
			t.Fatal("the dispatch was not executed")
		}

		select {
		case reply := <-replies:
			// Recompute the signature the way the platform verifies it:
			// from the delivered body bytes, per the locked contract.
			payload, err := pgaiv2sdk.DecodeJSON(reply.body)
			assert.NoError(t, err)

			canonical, err := pgaiv2sdk.CanonicalResult(payload, pgaiv2sdk.CommandReset)
			assert.NoError(t, err)

			expected := pgaiv2sdk.SignReplyMessage("42", "test-nonce", pgaiv2sdk.StatusDone,
				pgaiv2sdk.CommandReset, canonical, "", "", []byte(signingSecret))
			assert.Equal(t, expected, reply.signature)

			assert.Contains(t, string(reply.body), `"status":"done"`)
			assert.Contains(t, string(reply.body), `"command_id":"42"`)
		case <-time.After(5 * time.Second):
			t.Fatal("the signed reply was not delivered")
		}
	})
}

// panickyV2Executor simulates a runtime bug inside command execution.
type panickyV2Executor struct {
	fakeV2Executor
}

func (p *panickyV2Executor) ExecuteV2Command(context.Context,
	*pgaiv2sdk.DispatchRequest) (map[string]interface{}, error) {
	panic("simulated executor bug")
}

// TestProcessV2CommandRecoversPanic locks H3: a panic inside command
// execution must not crash the Joe process (processV2Command runs in a bare
// goroutine), and the platform must still receive a signed error reply.
func TestProcessV2CommandRecoversPanic(t *testing.T) {
	replies := make(chan []byte, 1)

	replyServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)

		replies <- body

		w.WriteHeader(http.StatusOK)
	}))
	defer replyServer.Close()

	assistant := &Assistant{
		credentialsCfg:   &config.Credentials{SigningSecret: "test-secret"},
		appCfg:           &config.Config{APIV2: config.APIV2{Enabled: true, ReplyHost: "127.0.0.1"}},
		v2ReplyTransport: replyServer.Client().Transport,
	}

	request := &pgaiv2sdk.DispatchRequest{
		SchemaVersion: pgaiv2sdk.SchemaVersion,
		CommandID:     "51",
		Command:       pgaiv2sdk.CommandReset,
		SessionID:     "31",
		Nonce:         "panic-nonce",
		ReplyURL:      replyServer.URL,
	}

	// Direct call (not via goroutine) so a non-recovered panic fails THIS
	// test instead of tearing down the process at a distance.
	assistant.processV2Command(&panickyV2Executor{}, request)

	select {
	case body := <-replies:
		payload, err := pgaiv2sdk.DecodeJSON(body)
		assert.NoError(t, err)
		assert.Equal(t, pgaiv2sdk.StatusError, payload["status"])
		assert.Equal(t, "51", payload["command_id"])
		errText, _ := payload["error"].(string)
		assert.NotContains(t, errText, "simulated executor bug",
			"the panic value must not leak into the platform-facing reply")
	case <-time.After(5 * time.Second):
		t.Fatal("no error reply was delivered after the executor panic")
	}
}

func TestV2CommandDeduper(t *testing.T) {
	deduper := &v2CommandDeduper{}
	base := time.Now()

	assert.False(t, deduper.markSeen("1", base), "first sighting is not a duplicate")
	assert.True(t, deduper.markSeen("1", base.Add(time.Minute)), "within the TTL is a duplicate")
	assert.False(t, deduper.markSeen("2", base.Add(time.Minute)), "distinct command IDs are independent")
	assert.False(t, deduper.markSeen("1", base.Add(v2SeenCommandTTL+2*time.Minute)),
		"after the TTL the command_id is forgotten (bounded cache)")
}

func TestV2AllowedReplyHosts(t *testing.T) {
	assistant := &Assistant{appCfg: &config.Config{}}

	t.Run("empty config fails closed", func(t *testing.T) {
		assert.Empty(t, assistant.v2AllowedReplyHosts())
		assert.Error(t, pgaiv2sdk.ValidateReplyURLHost("https://anywhere.example.com/x", assistant.v2AllowedReplyHosts()))
	})

	t.Run("derived from platform.url", func(t *testing.T) {
		assistant.appCfg.Platform.URL = "https://platform.example.com/api/general"
		assert.Equal(t, []string{"platform.example.com"}, assistant.v2AllowedReplyHosts())
	})

	t.Run("dedicated replyHost wins", func(t *testing.T) {
		assistant.appCfg.APIV2.ReplyHost = "callback.example.com"
		assert.Equal(t, []string{"callback.example.com"}, assistant.v2AllowedReplyHosts())
	})
}

func TestV2ReplySecret(t *testing.T) {
	assistant := &Assistant{
		credentialsCfg: &config.Credentials{SigningSecret: "workspace-secret"},
		appCfg:         &config.Config{},
	}

	t.Run("defaults to the workspace signing secret", func(t *testing.T) {
		assert.Equal(t, []byte("workspace-secret"), assistant.v2ReplySecret())
	})

	t.Run("dedicated replySecret wins", func(t *testing.T) {
		assistant.appCfg.APIV2.ReplySecret = "reply-secret"
		assert.Equal(t, []byte("reply-secret"), assistant.v2ReplySecret())
	})
}

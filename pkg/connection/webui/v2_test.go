/*
2026 © Postgres.ai
*/

package webui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"gitlab.com/postgres-ai/joe/pkg/config"
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

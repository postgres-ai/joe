package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "joe.yml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0600))
	return path
}

func TestLoadFile_ExpandsPlaceholders(t *testing.T) {
	t.Setenv("JOE_TEST_TOKEN", "secret-value")
	t.Setenv("JOE_TEST_EMPTY", "")

	tests := []struct {
		name string
		yaml string
		want string
	}{
		{"braced placeholder", `platform: {token: "${JOE_TEST_TOKEN}"}`, "secret-value"},
		{"bare shorthand", `platform: {token: "$JOE_TEST_TOKEN"}`, "secret-value"},
		{"escaped dollar", `platform: {token: "$$JOE_TEST_TOKEN"}`, "$JOE_TEST_TOKEN"},
		{"empty value", `platform: {token: "${JOE_TEST_EMPTY}"}`, ""},
		{"surrounding text", `platform: {token: "pre-${JOE_TEST_TOKEN}-post"}`, "pre-secret-value-post"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg Config
			_, err := LoadFile(writeConfig(t, tt.yaml), &cfg)
			require.NoError(t, err)
			assert.Equal(t, tt.want, cfg.Platform.Token)
		})
	}
}

// TestLoadFile_AcceptsAwkwardSecretChars proves the YAML-node path round-trips
// values that would have broken a byte-level expansion (quotes, colons, etc.).
func TestLoadFile_AcceptsAwkwardSecretChars(t *testing.T) {
	cases := map[string]string{
		"backslash":  `has\backslash`,
		"quote":      `has"quote`,
		"colon":      `has:colon`,
		"hash":       `has#hash`,
		"star":       `*starting-asterisk`,
		"ampersand":  `&starting-ampersand`,
		"dash":       `-leading-dash`,
		"whitespace": "  surrounding-spaces  ",
		"tab":        "with\ttab",
		"newline":    "line1\nline2",
	}

	for name, value := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv("JOE_TEST_AWKWARD", value)
			var cfg Config
			_, err := LoadFile(writeConfig(t, `platform: {token: "${JOE_TEST_AWKWARD}"}`), &cfg)
			require.NoError(t, err)
			assert.Equal(t, value, cfg.Platform.Token)
		})
	}
}

func TestLoadFile_RejectsMalformedPlaceholders(t *testing.T) {
	t.Setenv("JOE_TEST_SET", "ok")

	tests := []struct {
		name    string
		yaml    string
		wantSub string
	}{
		{"unclosed brace", `platform: {token: "${UNCLOSED"}`, "unterminated"},
		{"empty body", `platform: {token: "${}"}`, "empty"},
		{"name with dash", `platform: {token: "${BAD-NAME}"}`, "invalid placeholder name"},
		{"name with at-sign", `platform: {token: "${V@R}"}`, "invalid placeholder name"},
		{"name starting with digit", `platform: {token: "${1ABC}"}`, "invalid placeholder name"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg Config
			_, err := LoadFile(writeConfig(t, tt.yaml), &cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantSub)
			assert.Contains(t, err.Error(), "line")
		})
	}
}

func TestLoadFile_UnsetFailsWithLineColumn(t *testing.T) {
	var cfg Config
	body := "app:\n  debug: false\nplatform:\n  token: \"${JOE_TEST_DEFINITELY_NOT_SET}\"\n"
	_, err := LoadFile(writeConfig(t, body), &cfg)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrUnsetEnv))
	assert.Contains(t, err.Error(), "line 4")
	assert.Contains(t, err.Error(), "JOE_TEST_DEFINITELY_NOT_SET")
}

func TestLoadFile_FileNotFound(t *testing.T) {
	var cfg Config
	_, err := LoadFile(filepath.Join(t.TempDir(), "missing.yml"), &cfg)
	require.Error(t, err)
	assert.True(t, errors.Is(err, os.ErrNotExist))
}

func TestLoadFile_EmptyFile(t *testing.T) {
	var cfg Config
	_, err := LoadFile(writeConfig(t, ""), &cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestLoadFile_NonStringScalarsLeftAlone(t *testing.T) {
	var cfg Config
	_, err := LoadFile(writeConfig(t, "app:\n  debug: true\n  port: 8080\n"), &cfg)
	require.NoError(t, err)
	assert.True(t, cfg.App.Debug)
	assert.Equal(t, uint(8080), cfg.App.Port)
}

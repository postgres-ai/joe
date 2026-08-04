package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "joe.yml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0600))
	return path
}

// decodeSection re-parses the bytes LoadFile returns into loosely-typed values
// and hands back one top-level section. Config coerces every scalar to the
// field's type, which hides the tag a scalar actually ended up with; this keeps
// that visible so a test can assert on it.
func decodeSection(t *testing.T, expanded []byte, name string) map[string]interface{} {
	t.Helper()

	var doc map[string]interface{}
	require.NoError(t, yaml.Unmarshal(expanded, &doc))

	section, ok := doc[name].(map[string]interface{})
	require.True(t, ok, "section %q missing from the expanded config", name)

	return section
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

// TestLoadFile_UnquotedPlaceholderTakesValueType covers the typed settings an
// operator would reasonably want to drive from the environment. An unquoted
// placeholder loses the string tag the parser inferred from the placeholder
// text, so yaml re-resolves it from the expanded value.
func TestLoadFile_UnquotedPlaceholderTakesValueType(t *testing.T) {
	t.Setenv("JOE_TEST_PORT", "2500")
	t.Setenv("JOE_TEST_DEBUG", "true")
	t.Setenv("JOE_TEST_DURATION", "30s")
	t.Setenv("JOE_TEST_ENABLE", "true")

	var cfg Config

	body := "app:\n  port: ${JOE_TEST_PORT}\n  debug: ${JOE_TEST_DEBUG}\n" +
		"  minNotifyDuration: ${JOE_TEST_DURATION}\nregistration:\n  enable: ${JOE_TEST_ENABLE}\n"

	_, err := LoadFile(writeConfig(t, body), &cfg)
	require.NoError(t, err)
	assert.Equal(t, uint(2500), cfg.App.Port)
	assert.True(t, cfg.App.Debug)
	assert.Equal(t, 30*time.Second, cfg.App.MinNotifyDuration)
	assert.True(t, cfg.Registration.Enable)
}

// TestLoadFile_QuotedPlaceholderStaysString pins the other half of the rule: a
// quoted placeholder is a string whatever it resolves to, so a token that
// happens to be all digits does not silently become a number.
//
// The assertion runs against the expanded bytes rather than the decoded Config.
// yaml sets a Go string field from the scalar's literal text whichever type the
// tag resolved to, so a Config-level assertion passes either way and would not
// notice the carve-out disappearing.
func TestLoadFile_QuotedPlaceholderStaysString(t *testing.T) {
	t.Setenv("JOE_TEST_NUMERIC_TOKEN", "12345")

	var cfg Config

	expanded, err := LoadFile(writeConfig(t,
		"platform:\n  token: \"${JOE_TEST_NUMERIC_TOKEN}\"\n  project: ${JOE_TEST_NUMERIC_TOKEN}\n"), &cfg)
	require.NoError(t, err)
	assert.Equal(t, "12345", cfg.Platform.Token)

	platform := decodeSection(t, expanded, "platform")
	assert.IsType(t, "", platform["token"], "quoted placeholder must stay a string")
	assert.IsType(t, 0, platform["project"], "unquoted placeholder must take the resolved type")
}

// TestLoadFile_ExplicitTagWins covers the same gate from the other side. yaml
// already treats a quoted scalar as a string whatever tag it carries, so the
// style check earns its keep on the tagged form instead: an explicit !!str is
// not plain, keeps its tag through expansion, and beats the resolved value.
func TestLoadFile_ExplicitTagWins(t *testing.T) {
	t.Setenv("JOE_TEST_TAGGED_PORT", "2500")

	var cfg Config

	_, err := LoadFile(writeConfig(t, "app:\n  port: !!str ${JOE_TEST_TAGGED_PORT}\n"), &cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "into uint")
}

// TestLoadFile_MappingKeysStayStrings guards the carve-out for key position:
// re-typing a key would turn a Database Lab alias into a non-string and break
// the map it lives in. An alias resolving to a null literal is what makes the
// carve-out observable — an all-digit alias survives either way, because the
// decoder falls back to the key's literal text for a map[string]... .
func TestLoadFile_MappingKeysStayStrings(t *testing.T) {
	for _, alias := range []string{"1234", "0123", "null", "~"} {
		t.Run(alias, func(t *testing.T) {
			t.Setenv("JOE_TEST_ALIAS", alias)

			var cfg Config

			body := "channelMapping:\n  dblabServers:\n    ${JOE_TEST_ALIAS}:\n      url: https://dblab.example.com\n"

			_, err := LoadFile(writeConfig(t, body), &cfg)
			require.NoError(t, err)
			require.Contains(t, cfg.ChannelMapping.DBLabInstances, alias)
		})
	}
}

// TestLoadFile_KeyResolvingToMergeIndicator is the sharpest case for the key
// carve-out: "<<" only means "merge this map" while it is untagged, so dropping
// the tag on a key would let the value of an environment variable rewrite the
// document's structure instead of one of its values.
func TestLoadFile_KeyResolvingToMergeIndicator(t *testing.T) {
	t.Setenv("JOE_TEST_ALIAS", "<<")

	var cfg Config

	body := "defaults: &d\n  url: https://dblab.example.com\n" +
		"channelMapping:\n  dblabServers:\n    ${JOE_TEST_ALIAS}: *d\n"

	_, err := LoadFile(writeConfig(t, body), &cfg)
	require.NoError(t, err)
	require.Contains(t, cfg.ChannelMapping.DBLabInstances, "<<")
}

// TestLoadFile_NullResolvingPlaceholder covers the one value a re-typed scalar
// cannot represent: yaml resolves the null literals to nil, and the decoder
// short-circuits on nil before it reaches the string case, so dropping the tag
// would zero the field instead of storing the text that came back.
func TestLoadFile_NullResolvingPlaceholder(t *testing.T) {
	for _, value := range []string{"null", "Null", "NULL", "~"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("JOE_TEST_NULLISH", value)

			var cfg Config

			_, err := LoadFile(writeConfig(t, "platform:\n  project: ${JOE_TEST_NULLISH}\n"), &cfg)
			require.NoError(t, err)
			assert.Equal(t, value, cfg.Platform.Project)
		})
	}
}

// TestLoadFile_EmptyValueInTypedField keeps an empty variable from passing for
// a number: the scalar keeps its string tag, so the field fails to decode
// instead of staying zero and picking up the env-default afterwards.
func TestLoadFile_EmptyValueInTypedField(t *testing.T) {
	t.Setenv("JOE_TEST_BLANK", "")

	var cfg Config

	_, err := LoadFile(writeConfig(t, "app:\n  port: ${JOE_TEST_BLANK}\n"), &cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "into uint")
}

// TestLoadFile_CamelCaseKeysAreHonoured pins the two keys that the shipped
// example config spells in camelCase; both were silently dropped before they
// carried explicit yaml tags.
func TestLoadFile_CamelCaseKeysAreHonoured(t *testing.T) {
	var cfg Config

	body := "app:\n  minNotifyDuration: 30s\nchannelMapping:\n  dblabServers:\n    prod1:\n" +
		"      url: https://dblab.example.com\n      requestTimeout: 45s\n"

	_, err := LoadFile(writeConfig(t, body), &cfg)
	require.NoError(t, err)
	assert.Equal(t, 30*time.Second, cfg.App.MinNotifyDuration)
	assert.Equal(t, 45*time.Second, cfg.ChannelMapping.DBLabInstances["prod1"].RequestTimeout)
}

// TestLoadFile_UnsetErrorNamesEscape checks that a literal "$" in a password or
// token points the operator at the escape instead of leaving them to guess, and
// that the reported name is the one they can act on. os.Expand also reads the
// shell special variables as names, so a "$" followed by any of * # $ @ ! ? -
// or a digit names something nobody wrote — which is exactly where the hint has
// to carry the whole message.
func TestLoadFile_UnsetErrorNamesEscape(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		wantName string
	}{
		{"identifier after dollar", "P@ss$w0rd!", "w0rd"},
		{"digit after dollar", "cost$1k", "1"},
		{"hash after dollar", "a$#b", "#"},
		{"dash after dollar", "pa$-word", "-"},
		{"star after dollar", "pa$*word", "*"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg Config

			_, err := LoadFile(writeConfig(t, "platform:\n  token: \""+tt.value+"\"\n"), &cfg)
			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrUnsetEnv))
			assert.Contains(t, err.Error(), ErrUnsetEnv.Error()+": "+tt.wantName+" ")
			assert.Contains(t, err.Error(), escapeHint)
		})
	}
}

func TestLoadFile_RejectsDocumentWithoutSettings(t *testing.T) {
	for name, body := range map[string]string{
		"comment only":      "# nothing here\n",
		"whitespace only":   "  \n\n",
		"explicit null":     "null\n",
		"empty document":    "---\n",
		"document of tilde": "~\n",
	} {
		t.Run(name, func(t *testing.T) {
			var cfg Config
			_, err := LoadFile(writeConfig(t, body), &cfg)
			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrEmptyConfig))
		})
	}
}

// TestLoadFile_ScalarDocumentIsNotEmpty is the negative complement: only a null
// document counts as empty. A bare scalar is a malformed config and has to fail
// as a decode error, so widening the check from the null tag to "any scalar
// document" cannot slip through.
func TestLoadFile_ScalarDocumentIsNotEmpty(t *testing.T) {
	var cfg Config

	_, err := LoadFile(writeConfig(t, "42\n"), &cfg)
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrEmptyConfig))
}

// TestLoadFile_ShippedExampleConfig loads the file every operator copies, so a
// malformed placeholder or a key renamed out from under its struct tag fails
// here rather than on someone's first start.
func TestLoadFile_ShippedExampleConfig(t *testing.T) {
	for _, name := range []string{
		"PGAI_PLATFORM_ACCESS_TOKEN",
		"DBLAB_VERIFICATION_TOKEN",
		"WEBUI_SIGNING_SECRET",
		"SLACK_ACCESS_TOKEN",
		"SLACK_SIGNING_SECRET",
		"SLACK_RTM_ACCESS_TOKEN",
		"SLACK_SOCKET_ACCESS_TOKEN",
		"SLACK_APP_LEVEL_TOKEN",
	} {
		t.Setenv(name, "resolved-"+name)
	}

	var cfg Config

	_, err := LoadFile(filepath.Join("..", "..", "configs", "config.example.yml"), &cfg)
	require.NoError(t, err)

	assert.Equal(t, "resolved-PGAI_PLATFORM_ACCESS_TOKEN", cfg.Platform.Token)
	assert.Equal(t, uint(2400), cfg.App.Port)
	assert.Equal(t, 60*time.Second, cfg.App.MinNotifyDuration)

	require.Contains(t, cfg.ChannelMapping.DBLabInstances, "prod1")
	assert.Equal(t, "resolved-DBLAB_VERIFICATION_TOKEN", cfg.ChannelMapping.DBLabInstances["prod1"].Token)

	require.Len(t, cfg.ChannelMapping.CommunicationTypes["slacksm"], 1)
	slacksm := cfg.ChannelMapping.CommunicationTypes["slacksm"][0].Credentials
	assert.Equal(t, "resolved-SLACK_SOCKET_ACCESS_TOKEN", slacksm.AccessToken)
	assert.Equal(t, "resolved-SLACK_APP_LEVEL_TOKEN", slacksm.AppLevelToken)
}

// TestParseYAML_ReadsExpandedBytes covers the hand-off to a second decoder: the
// enterprise options provider re-parses what LoadFile returned, so typed
// placeholders have to survive the re-marshal too.
func TestParseYAML_ReadsExpandedBytes(t *testing.T) {
	t.Setenv("JOE_TEST_QUOTA", "20")
	t.Setenv("JOE_TEST_AUDIT", "true")

	var cfg Config

	body := "enterprise:\n  quota:\n    limit: ${JOE_TEST_QUOTA}\n  audit:\n    enabled: ${JOE_TEST_AUDIT}\n"

	expanded, err := LoadFile(writeConfig(t, body), &cfg)
	require.NoError(t, err)

	var container struct {
		Enterprise struct {
			Quota struct {
				Limit uint `yaml:"limit"`
			} `yaml:"quota"`
			Audit struct {
				Enabled bool `yaml:"enabled"`
			} `yaml:"audit"`
		} `yaml:"enterprise"`
	}

	require.NoError(t, ParseYAML(expanded, &container))
	assert.Equal(t, uint(20), container.Enterprise.Quota.Limit)
	assert.True(t, container.Enterprise.Audit.Enabled)
}

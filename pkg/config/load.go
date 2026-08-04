/*
2026 © Postgres.ai
*/

package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/ilyakaznacheev/cleanenv"
	"gopkg.in/yaml.v3"
)

// ErrUnsetEnv is returned when a config file references an environment
// variable that is not set.
var ErrUnsetEnv = errors.New("required environment variable is not set")

// ErrEmptyConfig is returned when a config file parses cleanly but carries no
// settings at all, which would otherwise surface much later as a nil map or a
// nil pointer dereference.
var ErrEmptyConfig = errors.New("config file has no configuration settings")

// escapeHint is appended to unset-variable errors: a stray "$" in a password or
// token reads as a placeholder, and the way out is not otherwise discoverable.
const escapeHint = `(write "$$" for a literal "$")`

// LoadFile reads path, expands ${VAR} / $VAR placeholders inside YAML string
// scalars from the environment, decodes the result into cfg, and applies
// env-tag overrides on top. The returned bytes are the expanded YAML so a
// second decoder (e.g. enterprise options) can reuse them without re-reading
// the filesystem. `$$` escapes to a literal `$`; unset variables fail.
//
// A placeholder written without quotes is re-typed after expansion, so it may
// stand in for a number, boolean, or duration as well as a string. Quoted
// placeholders always resolve to strings.
func LoadFile(path string, cfg interface{}) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	if len(data) == 0 {
		return nil, fmt.Errorf("config file %q is empty", path)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse YAML: %w", err)
	}

	if isEmptyDocument(&root) {
		return nil, fmt.Errorf("%w: %q", ErrEmptyConfig, path)
	}

	if err := expandNodes(&root, false); err != nil {
		return nil, err
	}

	if err := root.Decode(cfg); err != nil {
		return nil, fmt.Errorf("decode YAML: %w", err)
	}

	if err := cleanenv.ReadEnv(cfg); err != nil {
		return nil, fmt.Errorf("apply env overrides: %w", err)
	}

	return yaml.Marshal(&root)
}

// ParseYAML decodes YAML bytes (typically the expanded output of LoadFile)
// into cfg and applies env-tag overrides.
func ParseYAML(data []byte, cfg interface{}) error {
	if err := cleanenv.ParseYAML(bytes.NewReader(data), cfg); err != nil {
		return fmt.Errorf("parse YAML: %w", err)
	}

	if err := cleanenv.ReadEnv(cfg); err != nil {
		return fmt.Errorf("apply env overrides: %w", err)
	}

	return nil
}

// isEmptyDocument reports whether the parsed stream carries no settings: a file
// holding only comments or whitespace yields a zero node, and "null" or a bare
// "---" yields an explicit null document.
func isEmptyDocument(root *yaml.Node) bool {
	if root.Kind == 0 || len(root.Content) == 0 {
		return true
	}

	doc := root.Content[0]

	return doc.Kind == yaml.ScalarNode && doc.Tag == nullTag
}

// expandNodes walks the parsed document and resolves placeholders in every
// string scalar. isKey marks scalars sitting in a mapping's key position.
func expandNodes(n *yaml.Node, isKey bool) error {
	if n.Kind == yaml.ScalarNode && isStringTag(n.Tag) && strings.ContainsRune(n.Value, '$') {
		if err := validatePlaceholders(n.Value); err != nil {
			return fmt.Errorf("at line %d:%d: %w", n.Line, n.Column, err)
		}

		var missing []string

		n.Value = os.Expand(n.Value, func(name string) string {
			if name == "$" {
				return "$"
			}

			if v, ok := os.LookupEnv(name); ok {
				return v
			}

			if !slices.Contains(missing, name) {
				missing = append(missing, name)
			}

			return ""
		})
		if len(missing) > 0 {
			return fmt.Errorf("at line %d:%d: %w: %s %s",
				n.Line, n.Column, ErrUnsetEnv, strings.Join(missing, ", "), escapeHint)
		}

		// An unquoted placeholder stands in for the whole value, so drop the tag
		// the parser inferred from the placeholder text and let yaml re-resolve
		// it from what came back. Without this the scalar stays !!str and cannot
		// decode into a uint, bool, or duration field. Quoted scalars keep their
		// string tag, and keys stay strings so an all-digit key cannot become an
		// int and break the surrounding map.
		//
		// A value that re-resolves to !!null is left tagged too: the decoder
		// short-circuits on a null scalar, so dropping the tag there would zero
		// a string field and drop a map entry whose key resolved to "null"
		// instead of storing the literal text.
		if n.Style == 0 && !isKey && !isNullLiteral(n.Value) {
			n.Tag = ""
		}
	}

	for i, child := range n.Content {
		if err := expandNodes(child, n.Kind == yaml.MappingNode && i%2 == 0); err != nil {
			return err
		}
	}

	return nil
}

const (
	strTag  = "!!str"
	nullTag = "!!null"
)

func isStringTag(tag string) bool { return tag == "" || tag == strTag }

// isNullLiteral reports whether an expanded value would re-resolve to !!null.
// The empty string is included: yaml resolves it to null as well, and keeping
// the string tag turns an empty variable in a typed field into a decode error
// rather than a silent zero that the env-default then overwrites.
func isNullLiteral(value string) bool {
	switch value {
	case "", "~", "null", "Null", "NULL":
		return true
	}

	return false
}

const placeholderOpen = "${"

// validatePlaceholders rejects malformed ${...} occurrences so they surface
// as clear errors instead of being silently swallowed by os.Expand. Bare
// $VAR shorthand, $$ escapes, and well-formed ${VAR} are accepted.
func validatePlaceholders(s string) error {
	for i := 0; i < len(s); i++ {
		if s[i] != '$' {
			continue
		}

		if i+1 < len(s) && s[i+1] == '$' {
			i++
			continue
		}

		if !strings.HasPrefix(s[i:], placeholderOpen) {
			continue
		}

		nameStart := i + len(placeholderOpen)

		end := strings.IndexByte(s[nameStart:], '}')
		if end < 0 {
			return fmt.Errorf("unterminated ${...} placeholder")
		}

		nameEnd := nameStart + end
		if err := checkEnvName(s[nameStart:nameEnd]); err != nil {
			return err
		}

		i = nameEnd
	}

	return nil
}

func checkEnvName(name string) error {
	if name == "" {
		return fmt.Errorf("empty ${} placeholder")
	}

	for i, c := range name {
		ok := c == '_' ||
			(c >= 'A' && c <= 'Z') ||
			(c >= 'a' && c <= 'z') ||
			(c >= '0' && c <= '9' && i > 0)
		if !ok {
			return fmt.Errorf("invalid placeholder name %q", name)
		}
	}

	return nil
}

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

// LoadFile reads path, expands ${VAR} / $VAR placeholders inside YAML string
// scalars from the environment, decodes the result into cfg, and applies
// env-tag overrides on top. The returned bytes are the expanded YAML so a
// second decoder (e.g. enterprise options) can reuse them without re-reading
// the filesystem. `$$` escapes to a literal `$`; unset variables fail.
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

	if err := expandNodes(&root); err != nil {
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

func expandNodes(n *yaml.Node) error {
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
			return fmt.Errorf("at line %d:%d: %w: %s", n.Line, n.Column, ErrUnsetEnv, strings.Join(missing, ", "))
		}
	}

	for _, child := range n.Content {
		if err := expandNodes(child); err != nil {
			return err
		}
	}

	return nil
}

func isStringTag(tag string) bool { return tag == "" || tag == "!!str" }

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

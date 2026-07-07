/*
2026 © Postgres.ai
*/

// Package pgaiv2sdk implements the customer side of the Joe API v2 signed
// reply callback (Part of postgres-ai/platform-all#438, Stream G).
//
// It mirrors, byte-for-byte, the platform's shared canonicalizer
// public.joe_canonical_result (platform-all, db/functions/joe_canonical_result.sql)
// and the LOCKED reply-signing contract (CONTRACT_DECISIONS.md):
//
//	x-joe-signature: v0=hex(hmac-sha256('v0:' || command_id \n nonce \n status
//	  \n command \n joe_canonical_result(payload, command) \n coalesce(error,'')
//	  \n coalesce(live_conformance_digest,''), verify_token))
//
// Canonicalization rules (JCS/RFC-8785-style with LOCKED deviations):
//   - numbers are arbitrary-precision PLAIN DECIMAL (Postgres
//     trim_scale(numeric)::text): no exponent, trailing fraction zeros
//     stripped, -0 -> 0. Deliberately NOT the RFC-8785 ECMAScript float64
//     shortest-round-trip rendering;
//   - object keys sorted by byte / code-point order (collate "C");
//   - strings minimally escaped exactly like Postgres to_json(text):
//     `"`, `\`, \b \f \n \r \t, other control chars < 0x20 as \u00xx
//     (lowercase hex); DEL and non-ASCII bytes stay raw;
//   - a present JSON null -> the literal `null`; an ABSENT field -> the
//     empty string;
//   - fields inside joe_canonical_result are joined with LF (U+000A);
//   - plan_text is VERBATIM (never re-canonicalized).
//
// The byte contract is frozen by the cross-implementation signing vector
// (platform-all db/functions/test_joe_callback_signing_vector.sql); the same
// fixtures are asserted in this package's tests.
package pgaiv2sdk

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/pkg/errors"
)

// Commands supported by the Joe API v2 dispatch/reply loop.
const (
	CommandPlan      = "plan"
	CommandExplain   = "explain"
	CommandExec      = "exec"
	CommandHypo      = "hypo"
	CommandActivity  = "activity"
	CommandDescribe  = "describe"
	CommandTerminate = "terminate"
	CommandReset     = "reset"
)

// resultFields is the per-command result contract: the reply payload fields
// concatenated (LF-joined) into the 5th signed field, in this fixed order.
// Mirrors public.joe_canonical_result.
var resultFields = map[string][]string{
	CommandPlan:      {"plan_text", "plan_json"},
	CommandExplain:   {"plan_text", "plan_json"},
	CommandExec:      {"result_rows", "row_count", "notices"},
	CommandHypo:      {"hypo_plan", "hypo_used"},
	CommandActivity:  {"snapshot"},
	CommandDescribe:  {"snapshot"},
	CommandTerminate: {"terminated", "pid"},
	CommandReset:     {"reset"},
}

// IsSupportedCommand reports whether the v2 pipeline knows the command.
func IsSupportedCommand(command string) bool {
	_, ok := resultFields[strings.ToLower(command)]
	return ok
}

// CanonicalResult produces the 5th field of the signed 7-field concatenation:
// the command's result fields canonicalized and LF-joined. The payload is the
// DECODED reply body (as produced by DecodeJSON, so numbers are json.Number);
// an absent field canonicalizes to the empty string, a JSON null to `null`.
// plan_text is taken VERBATIM.
func CanonicalResult(payload map[string]interface{}, command string) (string, error) {
	fields, ok := resultFields[strings.ToLower(command)]
	if !ok {
		return "", errors.Errorf("unknown command %q", command)
	}

	parts := make([]string, 0, len(fields))

	for _, field := range fields {
		value, present := payload[field]

		// plan_text is verbatim: payload ->> 'plan_text' with coalesce('').
		if field == "plan_text" {
			verbatim, err := verbatimText(value, present)
			if err != nil {
				return "", err
			}

			parts = append(parts, verbatim)

			continue
		}

		if !present {
			// An absent field canonicalizes to the empty string.
			parts = append(parts, "")
			continue
		}

		canonical, err := Canonicalize(value)
		if err != nil {
			return "", errors.Wrapf(err, "failed to canonicalize field %q", field)
		}

		parts = append(parts, canonical)
	}

	return strings.Join(parts, "\n"), nil
}

// verbatimText extracts a string field verbatim (JSON string -> its value;
// null/absent -> ”), matching `coalesce(payload ->> 'plan_text', ”)`.
func verbatimText(value interface{}, present bool) (string, error) {
	if !present || value == nil {
		return "", nil
	}

	s, ok := value.(string)
	if !ok {
		return "", errors.Errorf("plan_text must be a JSON string, got %T", value)
	}

	return s, nil
}

// Canonicalize renders a decoded JSON value to its canonical byte form
// (public.joe_jcs_canonicalize). Accepted value types: nil, bool,
// json.Number, string, int, int64, []interface{}, map[string]interface{},
// and json.RawMessage (decoded first). float64 is rejected on purpose: the
// LOCKED contract requires arbitrary-precision decimal numbers, so callers
// must carry numbers as json.Number (DecodeJSON does).
func Canonicalize(value interface{}) (string, error) {
	b := &strings.Builder{}
	if err := canonicalizeValue(value, b); err != nil {
		return "", err
	}

	return b.String(), nil
}

func canonicalizeValue(value interface{}, b *strings.Builder) error {
	switch v := value.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		b.WriteString(strconv.FormatBool(v))
	case json.Number:
		return appendCanonicalNumber(b, v.String())
	case int:
		b.WriteString(strconv.Itoa(v))
	case int64:
		b.WriteString(strconv.FormatInt(v, 10))
	case string:
		appendJSONString(b, v)
	case json.RawMessage:
		decoded, err := DecodeJSONValue(v)
		if err != nil {
			return errors.Wrap(err, "failed to decode raw JSON")
		}

		return canonicalizeValue(decoded, b)
	case []interface{}:
		b.WriteByte('[')

		for i, elem := range v {
			if i > 0 {
				b.WriteByte(',')
			}

			if err := canonicalizeValue(elem, b); err != nil {
				return err
			}
		}

		b.WriteByte(']')
	case map[string]interface{}:
		return canonicalizeObject(v, b)
	case float64:
		return errors.New("float64 values are forbidden by the decimal-number contract; carry numbers as json.Number")
	default:
		return errors.Errorf("unsupported value type %T", value)
	}

	return nil
}

func canonicalizeObject(obj map[string]interface{}, b *strings.Builder) error {
	keys := make([]string, 0, len(obj))
	for key := range obj {
		keys = append(keys, key)
	}

	// Byte order == code-point order for UTF-8 strings (collate "C").
	sort.Strings(keys)

	b.WriteByte('{')

	for i, key := range keys {
		if i > 0 {
			b.WriteByte(',')
		}

		appendJSONString(b, key)
		b.WriteByte(':')

		if err := canonicalizeValue(obj[key], b); err != nil {
			return err
		}
	}

	b.WriteByte('}')

	return nil
}

// appendJSONString escapes a string exactly like Postgres to_json(text):
// minimal escapes for `"` and `\`, two-character escapes for \b \f \n \r \t,
// \u00xx (lowercase) for the remaining control chars below 0x20; every other
// byte — including DEL and non-ASCII UTF-8 — is emitted raw.
func appendJSONString(b *strings.Builder, s string) {
	const (
		hexDigits = "0123456789abcdef"
		// lowestPrintable is the first non-control byte; everything below it
		// (except the named escapes) is emitted as \u00xx.
		lowestPrintable = 0x20
	)

	b.WriteByte('"')

	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if c < lowestPrintable {
				b.WriteString(`\u00`)
				b.WriteByte(hexDigits[c>>4])
				b.WriteByte(hexDigits[c&0xf])
			} else {
				b.WriteByte(c)
			}
		}
	}

	b.WriteByte('"')
}

// appendCanonicalNumber renders a JSON number literal the way Postgres
// renders trim_scale(literal::numeric)::text: arbitrary-precision plain
// decimal, exponent expanded, trailing fraction zeros stripped, leading
// zeros dropped, and no negative zero.
func appendCanonicalNumber(b *strings.Builder, literal string) error {
	sign, digits, pointShift, err := parseNumberLiteral(literal)
	if err != nil {
		return err
	}

	intPart, fracPart := splitAtPoint(digits, pointShift)

	// Strip trailing zeros from the fraction (trim_scale) and leading zeros
	// from the integer part.
	fracPart = strings.TrimRight(fracPart, "0")
	intPart = strings.TrimLeft(intPart, "0")

	if intPart == "" {
		intPart = "0"
	}

	// -0 (and -0.000...) collapse to plain 0.
	if intPart == "0" && fracPart == "" {
		b.WriteByte('0')
		return nil
	}

	if sign {
		b.WriteByte('-')
	}

	b.WriteString(intPart)

	if fracPart != "" {
		b.WriteByte('.')
		b.WriteString(fracPart)
	}

	return nil
}

// parseNumberLiteral parses a JSON number literal into (negative sign, the
// bare digit string, point shift). The value equals
// digits * 10^(pointShift - len(digits)) — i.e. pointShift is the position
// of the decimal point counted from the LEFT edge of digits.
func parseNumberLiteral(literal string) (sign bool, digits string, pointShift int, err error) {
	rest := literal

	if strings.HasPrefix(rest, "-") {
		sign = true
		rest = rest[1:]
	}

	mantissa := rest
	exponent := 0

	if idx := strings.IndexAny(rest, "eE"); idx >= 0 {
		mantissa = rest[:idx]

		exponent, err = strconv.Atoi(strings.TrimPrefix(rest[idx+1:], "+"))
		if err != nil {
			return false, "", 0, errors.Wrapf(err, "invalid number literal %q", literal)
		}
	}

	intDigits, fracDigits, found := strings.Cut(mantissa, ".")
	if !found {
		fracDigits = ""
	}

	if intDigits == "" || !isDigits(intDigits) || (fracDigits != "" && !isDigits(fracDigits)) {
		return false, "", 0, errors.Errorf("invalid number literal %q", literal)
	}

	return sign, intDigits + fracDigits, len(intDigits) + exponent, nil
}

// splitAtPoint places the decimal point at pointShift (from the left edge of
// digits), zero-padding either side as needed.
func splitAtPoint(digits string, pointShift int) (intPart, fracPart string) {
	switch {
	case pointShift <= 0:
		return "0", strings.Repeat("0", -pointShift) + digits
	case pointShift >= len(digits):
		return digits + strings.Repeat("0", pointShift-len(digits)), ""
	default:
		return digits[:pointShift], digits[pointShift:]
	}
}

func isDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}

	return len(s) > 0
}

// DecodeJSON decodes a JSON object body preserving arbitrary-precision
// numbers (json.Number) — the only decode mode compatible with the decimal
// canonicalization contract.
func DecodeJSON(data []byte) (map[string]interface{}, error) {
	value, err := DecodeJSONValue(data)
	if err != nil {
		return nil, err
	}

	obj, ok := value.(map[string]interface{})
	if !ok {
		return nil, errors.Errorf("expected a JSON object, got %T", value)
	}

	return obj, nil
}

// DecodeJSONValue decodes any JSON value preserving numbers as json.Number.
func DecodeJSONValue(data []byte) (interface{}, error) {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()

	var value interface{}
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("failed to decode JSON: %w", err)
	}

	// Reject trailing garbage after the first value.
	if decoder.More() {
		return nil, errors.New("unexpected trailing data after JSON value")
	}

	return value, nil
}

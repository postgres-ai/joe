/*
2026 © Postgres.ai
*/

package msgproc

import (
	"strings"

	"github.com/pkg/errors"
)

// v2EnsureSingleStatement rejects SQL carrying more than one statement.
//
// The v2 plan/explain/hypo runners execute over the simple protocol, where a
// semicolon-bearing command string runs as a multi-statement batch: with the
// clone user unrestricted, a trailing `commit; <dml>` would escape even the
// rolled-back wrapper transaction and persist writes on the clone (M5
// no-commit invariant; rev609/rev210 ship-blocker 3).
//
// The scan understands single-quoted strings (with ” doubling; backslashes
// are deliberately NOT treated as escapes, matching
// standard_conforming_strings=on — for E” strings this can only over-reject,
// never under-reject), double-quoted identifiers (with "" doubling),
// dollar-quoted bodies ($tag$ ... $tag$), line comments (-- ...), and nested
// block comments (/* ... */). A top-level semicolon followed by anything but
// whitespace/comments is a multi-statement batch; trailing semicolons are
// harmless and accepted.
func v2EnsureSingleStatement(sql string) error {
	sawStatementEnd := false

	for i := 0; i < len(sql); {
		c := sql[i]

		// Whitespace and comments are insignificant anywhere, including
		// after a trailing semicolon.
		switch {
		case isV2SQLSpace(c):
			i++
			continue
		case c == '-' && i+1 < len(sql) && sql[i+1] == '-':
			i = skipV2LineComment(sql, i)
			continue
		case c == '/' && i+1 < len(sql) && sql[i+1] == '*':
			i = skipV2BlockComment(sql, i)
			continue
		case c == ';':
			sawStatementEnd = true
			i++

			continue
		}

		if sawStatementEnd {
			return errors.New("multi-statement SQL is not allowed for this v2 command")
		}

		switch {
		case c == '\'':
			i = skipV2QuotedRun(sql, i, '\'')
		case c == '"':
			i = skipV2QuotedRun(sql, i, '"')
		case c == '$':
			if end, ok := skipV2DollarQuote(sql, i); ok {
				i = end
			} else {
				i++
			}
		default:
			i++
		}
	}

	return nil
}

// skipV2QuotedRun advances past a quoted run opened at start by the given
// quote byte, honoring doubling (” or "") as an escaped quote. An
// unterminated run consumes the rest of the input (which then contains no
// further top-level semicolons — the safe direction).
func skipV2QuotedRun(sql string, start int, quote byte) int {
	i := start + 1

	for i < len(sql) {
		if sql[i] != quote {
			i++
			continue
		}

		if i+1 < len(sql) && sql[i+1] == quote {
			i += 2
			continue
		}

		return i + 1
	}

	return i
}

// skipV2DollarQuote advances past a $tag$ ... $tag$ dollar-quoted body
// opened at start. It reports false when start is not a dollar-quote opener
// (e.g. the $1 of a parameter placeholder).
func skipV2DollarQuote(sql string, start int) (int, bool) {
	tagEnd := start + 1

	for tagEnd < len(sql) && isV2DollarTagByte(sql[tagEnd]) {
		tagEnd++
	}

	if tagEnd >= len(sql) || sql[tagEnd] != '$' {
		return 0, false
	}

	delimiter := sql[start : tagEnd+1]

	bodyEnd := strings.Index(sql[tagEnd+1:], delimiter)
	if bodyEnd < 0 {
		// Unterminated: consume the rest (the safe direction).
		return len(sql), true
	}

	return tagEnd + 1 + bodyEnd + len(delimiter), true
}

// skipV2LineComment advances past a -- comment opened at start.
func skipV2LineComment(sql string, start int) int {
	if idx := strings.IndexByte(sql[start:], '\n'); idx >= 0 {
		return start + idx + 1
	}

	return len(sql)
}

// skipV2BlockComment advances past a /* ... */ comment opened at start,
// honoring nesting like Postgres does. An unterminated comment consumes the
// rest of the input.
func skipV2BlockComment(sql string, start int) int {
	depth := 0
	i := start

	for i < len(sql) {
		switch {
		case i+1 < len(sql) && sql[i] == '/' && sql[i+1] == '*':
			depth++
			i += 2
		case i+1 < len(sql) && sql[i] == '*' && sql[i+1] == '/':
			depth--
			i += 2

			if depth == 0 {
				return i
			}
		default:
			i++
		}
	}

	return i
}

func isV2SQLSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v'
}

func isV2DollarTagByte(c byte) bool {
	return c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

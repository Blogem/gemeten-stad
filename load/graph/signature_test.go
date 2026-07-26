package graph

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAssertSafeIRI covers the SPARQL-injection security gate: an IRI read back out of the store
// (STR(?s) in signatureQuery) must be rejected loudly before it is ever re-embedded, unescaped,
// into a `<...>` token by iriValuesList. Jena 5.5 accepts and decodes Turtle IRIREF UCHAR escapes
// on ingest, so a stored subject IRI can carry raw `>`/space/control characters even though this
// writer never mints one that does — assertSafeIRI is the choke point that catches that.
func TestAssertSafeIRI(t *testing.T) {
	tests := []struct {
		name    string
		id      iri
		wantErr bool
	}{
		{
			name: "ordinary data: IRI is accepted",
			id:   "http://gemetenstad.nl/id/tree1",
		},
		{
			name: "ordinary run: IRI is accepted",
			id:   "http://gemetenstad.nl/run/load-20240101T000000.000000000Z",
		},
		{
			name: "IRI with query-like path segments but no disallowed characters is accepted",
			id:   "http://gemetenstad.nl/id/tree-1_v2.final",
		},
		{
			name:    "confirmed live payload: decoded UCHAR breakout with raw '>' and ';DROP ALL;' is rejected",
			id:      "http://gemetenstad.nl/id/tree1> } };DROP ALL;#",
			wantErr: true,
		},
		{
			name:    "embedded raw '>' is rejected",
			id:      "http://gemetenstad.nl/id/tree1>evil",
			wantErr: true,
		},
		{
			name:    "embedded raw '<' is rejected",
			id:      "http://gemetenstad.nl/id/<evil",
			wantErr: true,
		},
		{
			name:    "embedded space is rejected",
			id:      "http://gemetenstad.nl/id/tree 1",
			wantErr: true,
		},
		{
			name:    "embedded double quote is rejected",
			id:      `http://gemetenstad.nl/id/tree"1`,
			wantErr: true,
		},
		{
			name:    "embedded backslash is rejected",
			id:      `http://gemetenstad.nl/id/tree\1`,
			wantErr: true,
		},
		{
			name:    "embedded curly braces are rejected",
			id:      "http://gemetenstad.nl/id/tree{1}",
			wantErr: true,
		},
		{
			name:    "embedded control character (newline) is rejected",
			id:      "http://gemetenstad.nl/id/tree\n1",
			wantErr: true,
		},
		{
			name:    "embedded control character (NUL) is rejected",
			id:      "http://gemetenstad.nl/id/tree\x001",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := assertSafeIRI(tt.id)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "graph:", "error should carry the package's loud-failure prefix")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestLiteralTerm covers the literal-embedding escaper: the output must always be a
// well-formed, non-breakoutable SPARQL string-literal term — in particular the closing quote
// must never be swallowed by an unescaped trailing backslash (the bug this fix replaces), and a
// value containing a literal double quote must not terminate the term early.
func TestLiteralTerm(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		datatype string

		wantErr bool
		// want, if non-empty, is the exact expected rendered term.
		want string
	}{
		{
			name:  "plain string, no datatype",
			value: "2024-01-01",
			want:  `"2024-01-01"`,
		},
		{
			name:     "typed literal with datatype",
			value:    "2024-01-01",
			datatype: "http://www.w3.org/2001/XMLSchema#date",
			want:     `"2024-01-01"^^<http://www.w3.org/2001/XMLSchema#date>`,
		},
		{
			name:  "embedded double quote is escaped",
			value: `say "hi"`,
			want:  `"say \"hi\""`,
		},
		{
			name:  "value ending in a bare backslash does not swallow the closing quote",
			value: `X\`,
			want:  `"X\\"`,
		},
		{
			name:  "embedded newline is escaped",
			value: "line1\nline2",
			want:  `"line1\nline2"`,
		},
		{
			name:     "unsafe datatype IRI is rejected",
			value:    "2024-01-01",
			datatype: "http://gemetenstad.nl/ns#date> } };DROP ALL;#",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := literalTerm(tt.value, tt.datatype)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)

			// Structural guarantee, independent of the exact escaping scheme: the rendered
			// term must start and end with an unescaped double quote, and every backslash
			// and double quote in between must be escaped — i.e. the term parses as exactly
			// one SPARQL string literal, not a truncated/broken-out one.
			assertWellFormedQuotedLiteral(t, got)
		})
	}
}

// assertWellFormedQuotedLiteral asserts that term begins with `"`, and that the first
// unescaped `"` scanning left-to-right from index 1 is the LAST character of the leading
// quoted-string portion (i.e. there is no unescaped quote in the middle that would let an
// injected payload break out of the string early or leave a dangling unterminated literal).
func assertWellFormedQuotedLiteral(t *testing.T, term string) {
	t.Helper()
	require.True(t, strings.HasPrefix(term, `"`), "term must start with a quote: %q", term)

	body := term[1:]
	i := 0
	for i < len(body) {
		switch body[i] {
		case '\\':
			// An escape sequence consumes two bytes; a trailing lone backslash here would
			// mean the escaper failed to double it (the exact bug being regression-tested).
			require.True(t, i+1 < len(body), "unescaped trailing backslash before closing quote in %q", term)
			i += 2
		case '"':
			// Found the unescaped closing quote: everything after it must be either
			// nothing (plain literal) or a well-formed ^^<datatype> suffix.
			rest := body[i+1:]
			require.True(t, rest == "" || strings.HasPrefix(rest, "^^<"), "unexpected trailing content after closing quote in %q: %q", term, rest)
			return
		default:
			i++
		}
	}
	t.Fatalf("no unescaped closing quote found in term %q", term)
}

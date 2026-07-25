package shared

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRedactArgs covers the secret-redaction helper used before logging CLI args: any
// `password=` token must be masked end-to-end, everything else passes through untouched.
func TestRedactArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "masks the password token in a DSN-style arg, leaving the rest intact",
			args: []string{"ingest", "PG:host=db port=5432 dbname=gemeten_stad user=gs password=gs"},
			want: []string{"ingest", "PG:host=db port=5432 dbname=gemeten_stad user=gs password=***REDACTED***"},
		},
		{
			name: "masks a password= arg that is the whole token",
			args: []string{"password=supersecret"},
			want: []string{"password=***REDACTED***"},
		},
		{
			name: "masks multiple password-bearing args independently",
			args: []string{"password=one", "--reset", "password=two"},
			want: []string{"password=***REDACTED***", "--reset", "password=***REDACTED***"},
		},
		{
			name: "returns args without password= verbatim",
			args: []string{"ingest", "--reset", "load"},
			want: []string{"ingest", "--reset", "load"},
		},
		{
			name: "empty input yields no elements",
			args: []string{},
			want: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := redactArgs(tt.args)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestRedactArgsMasksSecretValue asserts the actual secret value never survives redaction, not
// just that *some* masking happened.
func TestRedactArgsMasksSecretValue(t *testing.T) {
	args := []string{"PG:host=db port=5432 dbname=gemeten_stad user=gs password=gs"}

	got := redactArgs(args)
	require.Len(t, got, 1)

	joined := got[0]
	assert.NotContains(t, joined, "password=gs", "the real password must not appear after redaction")
	assert.Contains(t, joined, "password=***REDACTED***")
	// The rest of the DSN survives redaction.
	assert.True(t, strings.HasPrefix(joined, "PG:host=db port=5432 dbname=gemeten_stad user=gs "))
}

// TestRedactArgsDoesNotMutateInput guards against redactArgs mutating the caller's slice in
// place (e.g. via a shared backing array), which would leak the secret to any other holder of
// the original slice.
func TestRedactArgsDoesNotMutateInput(t *testing.T) {
	original := []string{"PG:host=db password=gs", "safe-arg"}
	snapshot := append([]string(nil), original...)

	_ = redactArgs(original)

	assert.Equal(t, snapshot, original, "redactArgs must not mutate its input slice")
}

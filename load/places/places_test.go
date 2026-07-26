package places

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAssertSafeIdentificatie covers the assertSafeIdentificatie guard directly: a safe numeric
// gebieden identificatie must pass, and an identificatie carrying a disallowed IRIREF character
// (an embedded ">", a space, a control character, or one of the backslash/quote/backtick
// characters) must return an error naming the offending character rather than panicking (the
// guard moved from a panic deep in the pure render path to an error-returning check BuildCandidate
// runs at the IO boundary, mirroring load/graph/signature.go's assertSafeIRI).
func TestAssertSafeIdentificatie(t *testing.T) {
	tests := []struct {
		name          string
		identificatie string
		wantErr       bool
	}{
		{
			name:          "safe numeric identificatie",
			identificatie: "03630000000123",
			wantErr:       false,
		},
		{
			name:          "embedded greater-than",
			identificatie: "0363>0000000123",
			wantErr:       true,
		},
		{
			name:          "embedded space",
			identificatie: "0363 0000000123",
			wantErr:       true,
		},
		{
			name:          "embedded control character",
			identificatie: "0363\x000000000123",
			wantErr:       true,
		},
		{
			name:          "embedded backslash",
			identificatie: `0363\0000000123`,
			wantErr:       true,
		},
		{
			name:          "embedded double quote",
			identificatie: `0363"0000000123`,
			wantErr:       true,
		},
		{
			name:          "embedded backtick",
			identificatie: "0363`0000000123",
			wantErr:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := assertSafeIdentificatie(tt.identificatie)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "unsafe gebieden identificatie")
				return
			}
			assert.NoError(t, err)
		})
	}
}

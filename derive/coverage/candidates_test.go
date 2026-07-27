package coverage

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// NOTE (tester, pass 1): this file covers only the PURE windowing helpers
// (windowEnd, inWindow) — [publication, publication+3yr] arithmetic with no
// I/O. The SRID-aligned metric distance (permit resolved point -> felling
// geometry, "Requirement: Generate candidate permit↔felling pairs", scenario
// "A resolved point yields a per-felling distance") is computed inside
// PostGIS and is DB-backed; it belongs to the integration lane (task 5.4),
// not here.

// -- inWindow: pub <= felling <= windowEnd(pub); felling < pub is excluded --

func TestInWindow(t *testing.T) {
	pub := time.Date(2022, 3, 10, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		pub     time.Time
		felling time.Time
		want    bool
	}{
		{
			name:    "felling on the publication date is included",
			pub:     pub,
			felling: pub,
			want:    true,
		},
		{
			name:    "felling one day before publication is excluded",
			pub:     pub,
			felling: pub.AddDate(0, 0, -1),
			want:    false,
		},
		{
			name:    "felling well within the 3yr window is included",
			pub:     pub,
			felling: pub.AddDate(1, 0, 0),
			want:    true,
		},
		{
			name:    "felling exactly at windowEnd is included",
			pub:     pub,
			felling: windowEnd(pub),
			want:    true,
		},
		{
			name:    "felling one day after windowEnd is excluded",
			pub:     pub,
			felling: windowEnd(pub).AddDate(0, 0, 1),
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, inWindow(tt.pub, tt.felling))
		})
	}
}

// -- windowEnd: pub.AddDate(3, 0, 0), including Go's leap-day normalization --

func TestWindowEnd(t *testing.T) {
	tests := []struct {
		name string
		pub  time.Time
		// wantFormat pins the value time.Time.AddDate(3,0,0) actually normalizes
		// to on this Go toolchain (verified via `go run` against go1.26.1:
		// 2020-02-29.AddDate(3,0,0) == 2023-03-01, since 2023 is not a leap year
		// and Go overflows the nonexistent Feb 29 into March 1) — a visible
		// regression anchor, not a guessed calendar date.
		wantFormat string
	}{
		{
			name:       "an ordinary date advances exactly 3 calendar years",
			pub:        time.Date(2022, 6, 15, 0, 0, 0, 0, time.UTC),
			wantFormat: "2025-06-15",
		},
		{
			name:       "a leap-day publication overflows Feb 29 into Mar 1 three (non-leap) years later",
			pub:        time.Date(2020, 2, 29, 0, 0, 0, 0, time.UTC),
			wantFormat: "2023-03-01",
		},
		{
			name:       "a leap-day publication four years out still overflows (2024 is a leap year but the +3y arithmetic targets 2027)",
			pub:        time.Date(2024, 2, 29, 0, 0, 0, 0, time.UTC),
			wantFormat: "2027-03-01",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Ground truth computed the same way the implementation is specified
			// to compute it (pub.AddDate(3,0,0)), per the API contract — not a
			// hand-derived calendar date.
			want := tt.pub.AddDate(3, 0, 0)

			got := windowEnd(tt.pub)

			assert.True(t, got.Equal(want), "windowEnd(%s) = %s, want %s", tt.pub, got, want)
			assert.Equal(t, tt.wantFormat, got.Format("2006-01-02"))
		})
	}
}

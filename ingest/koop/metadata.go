package koop

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/Blogem/gemeten-stad/ingest/shared"
)

// metadataBaseURL is the host serving each publication's metadata.xml sidecar (DATA_SOURCES.md §1).
// The sidecar carries OVERHEIDop.referentienummer (the zaaknummer), OVERHEIDop.activiteit, and
// OVERHEIDop.gebiedsmarkering — the zaaknummer being absent from the SRU record P11 lands.
const metadataBaseURL = "https://zoek.officielebekendmakingen.nl/"

// Rate-limit + retry policy for the metadata.xml sidecar fetch. A full backfill issues one fetch
// per publication (~10k), and the host resets connections (EOF / unexpected EOF) under rapid-fire
// load, so fetches are paced by metadataRateInterval() and each is retried with capped exponential
// backoff — mirroring ingest/shared's SRU-page policy. A genuine 404 (shared.ErrNotFound) is NOT
// retried; only transient transport failures are.
const (
	defaultMetadataRateMillis = 200 // pacing default, matching shared.SRURateInterval
	metadataMaxAttempts       = 8
	metadataRetryBaseDelay    = 1 * time.Second
	metadataMaxRetryDelay     = 20 * time.Second
)

// metadataSleep is a seam over time.Sleep so tests can drive the pacing/backoff without waiting.
var metadataSleep = func(d time.Duration) { time.Sleep(d) }

// errMetadataExhausted signals that a sidecar fetch failed transiently on every attempt (its whole
// backoff chain never recovered). It is NOT fatal on its own — landMetadata leaves the sidecar
// unlanded so a later run retries it — but the harvest loop counts CONSECUTIVE occurrences as a
// block signal and aborts (circuit breaker, see Ingest / metadataMaxConsecutiveFails).
var errMetadataExhausted = errors.New("koop: metadata sidecar unavailable after all retries")

// defaultMetadataMaxConsecutiveFails is the circuit-breaker threshold: this many sidecar fetches in
// a row exhausting their retries (with no success or definitive 404 in between) means the host is
// almost certainly blocking us, so the harvest aborts rather than hammering on.
const defaultMetadataMaxConsecutiveFails = 2

// metadataMaxConsecutiveFails resolves the circuit-breaker threshold, overridable via
// GS_KOOP_METADATA_MAX_CONSECUTIVE_FAILS. 0 disables the breaker (grind through, leaving failures
// unlanded for a later run); a missing/invalid/negative value falls back to the default (2).
func metadataMaxConsecutiveFails() int {
	if v := os.Getenv("GS_KOOP_METADATA_MAX_CONSECUTIVE_FAILS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}
	}
	return defaultMetadataMaxConsecutiveFails
}

// metadataRateInterval is the pacing delay between successive sidecar fetches. It defaults to 200ms
// but is overridable via GS_KOOP_METADATA_RATE_MS (milliseconds) so a large sustained backfill can
// be run gentler against the aggressively-throttling host without a rebuild (design D5). A missing,
// empty, non-numeric, or negative value falls back to the default.
func metadataRateInterval() time.Duration {
	if v := os.Getenv("GS_KOOP_METADATA_RATE_MS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return time.Duration(n) * time.Millisecond
		}
	}
	return defaultMetadataRateMillis * time.Millisecond
}

// metadataURL builds the metadata.xml sidecar URL for a publication id, e.g.
// "https://zoek.officielebekendmakingen.nl/gmb-2022-291126/metadata.xml".
func metadataURL(id string) string {
	return metadataBaseURL + id + "/metadata.xml"
}

// metadataArtifactName is the landing name of a publication's metadata sidecar, keyed by the same
// id as the SRU record but distinguished by the ".metadata.xml" suffix so the load stage can tell
// the two verbatim artifacts apart (koop/<id>.xml vs koop/<id>.metadata.xml).
func metadataArtifactName(id string) string {
	return "koop/" + id + ".metadata.xml"
}

// metadataRetryBackoff computes the capped exponential backoff after a failed attempt n (1-based):
// min(metadataRetryBaseDelay<<(n-1), metadataMaxRetryDelay). Mirrors shared.sruRetryBackoff.
func metadataRetryBackoff(attempt int) time.Duration {
	delay := metadataRetryBaseDelay
	for i := 1; i < attempt; i++ {
		if delay >= metadataMaxRetryDelay {
			return metadataMaxRetryDelay
		}
		delay *= 2
	}
	if delay > metadataMaxRetryDelay {
		return metadataMaxRetryDelay
	}
	return delay
}

// fetchMetadata fetches url, retrying transient transport failures with capped backoff. It returns
// (body, found, err): found==false with a nil err means the sidecar is definitively absent (HTTP
// 404, shared.ErrNotFound) — not retried; a non-nil err means every attempt failed transiently.
func fetchMetadata(ctx context.Context, httpGet shared.HTTPGetFunc, url string) (body []byte, found bool, err error) {
	var lastErr error
	for attempt := 1; attempt <= metadataMaxAttempts; attempt++ {
		rc, getErr := httpGet(ctx, url)
		if getErr != nil {
			if errors.Is(getErr, shared.ErrNotFound) {
				return nil, false, nil
			}
			lastErr = getErr
		} else {
			b, readErr := io.ReadAll(rc)
			closeErr := rc.Close()
			switch {
			case readErr != nil:
				lastErr = readErr
			case closeErr != nil:
				lastErr = closeErr
			default:
				return b, true, nil
			}
		}
		if attempt < metadataMaxAttempts {
			delay := metadataRetryBackoff(attempt)
			slog.Warn("koop: metadata sidecar fetch failed; retrying",
				"url", url, "attempt", attempt, "maxAttempts", metadataMaxAttempts, "backoff", delay, "error", lastErr)
			metadataSleep(delay)
		}
	}
	return nil, false, fmt.Errorf("koop: fetch metadata %s after %d attempts: %w", url, metadataMaxAttempts, lastErr)
}

// landMetadata fetches and lands publication id's metadata.xml sidecar verbatim (D1), keyed by id.
// It is gated on need (D2): the sidecar is fetched only when the SRU record was (re)landed this run
// (recordChanged) or the sidecar is not yet present — so an unchanged re-run performs no redundant
// sidecar fetch, while a sidecar that failed to land on an earlier run is retried on the next.
//
// Fetches are paced by metadataRateInterval() and retried on transient errors (fetchMetadata). A
// definitive 404 is logged and skipped (returns nil); a whole backoff chain failing transiently is
// logged, left unlanded (so the next run retries it), and returned as errMetadataExhausted so the
// harvest loop's circuit breaker can count consecutive occurrences (a block signal). An invalid
// identifier is skipped defensively (it never reaches URL/path construction) — landRecord has
// already logged it. Store/IO failures are returned as ordinary (fatal) errors.
func landMetadata(ctx context.Context, store *shared.RawStore, httpGet shared.HTTPGetFunc, id string, fetchedAt time.Time, recordChanged bool) error {
	if !validIdentifier.MatchString(id) {
		return nil
	}

	name := metadataArtifactName(id)

	landed, err := store.Landed(name)
	if err != nil {
		return fmt.Errorf("koop: check landed %q: %w", name, err)
	}
	if landed && !recordChanged {
		return nil
	}

	metadataSleep(metadataRateInterval())

	url := metadataURL(id)
	body, found, err := fetchMetadata(ctx, httpGet, url)
	if err != nil {
		// Whole backoff chain failed transiently: leave the sidecar unlanded (a later run retries
		// it) and signal errMetadataExhausted so the harvest loop's circuit breaker can count it.
		slog.Warn("koop: metadata sidecar unavailable after retries; will retry on a later run", "id", id, "error", err)
		return errMetadataExhausted
	}
	if !found {
		// Definitive 404: this publication has no metadata sidecar (D3). Skip, don't fail.
		slog.Info("koop: publication has no metadata sidecar", "id", id)
		return nil
	}
	if len(body) == 0 {
		slog.Warn("koop: empty metadata sidecar; landing record without it", "id", id)
		return nil
	}

	if landed {
		unchanged, err := contentUnchanged(store, name, body)
		if err != nil {
			return err
		}
		if unchanged {
			return nil
		}
	}

	if _, err := store.Land(name, bytes.NewReader(body), url, fetchedAt); err != nil {
		return fmt.Errorf("koop: land %q: %w", name, err)
	}
	return nil
}

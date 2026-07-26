package koop

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/Blogem/gemeten-stad/ingest/shared"
)

// metadataBaseURL is the host serving each publication's metadata.xml sidecar (DATA_SOURCES.md §1).
// The sidecar carries OVERHEIDop.referentienummer (the zaaknummer), OVERHEIDop.activiteit, and
// OVERHEIDop.gebiedsmarkering — the zaaknummer being absent from the SRU record P11 lands.
const metadataBaseURL = "https://zoek.officielebekendmakingen.nl/"

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

// landMetadata fetches and lands publication id's metadata.xml sidecar verbatim (D1), keyed by id.
// It is gated on need (D2): the sidecar is fetched only when the SRU record was (re)landed this run
// (recordChanged) or the sidecar is not yet present — so an unchanged re-run performs no redundant
// sidecar fetch, while a sidecar that failed to land on an earlier run is retried on the next.
//
// A missing/unavailable sidecar is non-fatal (D3): some older ids return none, and the SRU record
// has already been landed regardless, so the absence is logged and skipped rather than failing the
// harvest or dropping the publication. An invalid identifier is skipped defensively (it never
// reaches URL/path construction) — landRecord has already logged it.
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

	url := metadataURL(id)
	rc, err := httpGet(ctx, url)
	if err != nil {
		// Non-fatal: the SRU record is already landed; record the absence and move on (D3).
		slog.Warn("koop: metadata sidecar unavailable; landing record without it", "id", id, "err", err)
		return nil
	}
	body, readErr := io.ReadAll(rc)
	closeErr := rc.Close()
	if readErr != nil {
		slog.Warn("koop: read metadata sidecar failed; landing record without it", "id", id, "err", readErr)
		return nil
	}
	if closeErr != nil {
		return fmt.Errorf("koop: close metadata body for %q: %w", id, closeErr)
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

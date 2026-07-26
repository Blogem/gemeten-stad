// Package dump exports and restores snapshots of the graph, PostGIS, and the NER cache.
//
// The NER cache on-disk contract (design D4): a directory under GS_NER_CACHE_PATH (defaulting to
// "<GS_RAW_DATA_PATH>/ner-cache") holding one file per cache entry, keyed by document id + model/
// prompt version, so a re-run under a new model/prompt version is a distinct entry rather than a
// stale hit. Phase 2's `extract` stage is the writer; dump only captures/restores the directory
// verbatim as a tar+gzip archive (nercache.go) — an absent directory is a valid, empty snapshot.
package dump

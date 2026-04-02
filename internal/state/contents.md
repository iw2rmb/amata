[store.go](store.go) Event-sourced run state store that appends NDJSON events, applies transitions, and rebuilds/persists snapshots.
[store_test.go](store_test.go) Verifies append-only event logging, snapshot rebuild/corruption recovery, and step/frame sequence bookkeeping.

// Package eventstore persists inventory events in an append-only log.
// Writes use optimistic concurrency: Append succeeds only if the stream is
// still at expectedVersion, so two writers can never both reserve the last unit.
package eventstore

import (
	"context"
	"fmt"
	"sync"

	"github.com/Yang-Zhang1994/stockledger/internal/inventory"
)

// ErrConflict wraps inventory.ErrConflict so the service can retry on it.
var ErrConflict = fmt.Errorf("stream changed since it was loaded: %w", inventory.ErrConflict)

type Store interface {
	Load(ctx context.Context, sku string) ([]inventory.Event, error)
	Append(ctx context.Context, sku string, expectedVersion int, events []inventory.Event) error
	// Unpublished returns events not yet handed to the broker, oldest first (the outbox).
	Unpublished(ctx context.Context, limit int) ([]Record, error)
	MarkPublished(ctx context.Context, seqs []int64) error
}

// Record is an event plus its global sequence number in the log.
type Record struct {
	Seq   int64
	Event inventory.Event
}

// Memory is an in-process Store for tests and the -mem demo mode.
type Memory struct {
	mu        sync.Mutex
	streams   map[string][]inventory.Event
	log       []Record
	published map[int64]bool
}

func NewMemory() *Memory {
	return &Memory{streams: map[string][]inventory.Event{}, published: map[int64]bool{}}
}

func (m *Memory) Load(_ context.Context, sku string) ([]inventory.Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]inventory.Event(nil), m.streams[sku]...), nil
}

func (m *Memory) Append(_ context.Context, sku string, expectedVersion int, events []inventory.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.streams[sku]) != expectedVersion {
		return ErrConflict
	}
	for _, e := range events {
		m.streams[sku] = append(m.streams[sku], e)
		m.log = append(m.log, Record{Seq: int64(len(m.log) + 1), Event: e})
	}
	return nil
}

func (m *Memory) Unpublished(_ context.Context, limit int) ([]Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Record
	for _, r := range m.log {
		if !m.published[r.Seq] {
			out = append(out, r)
			if len(out) == limit {
				break
			}
		}
	}
	return out, nil
}

func (m *Memory) MarkPublished(_ context.Context, seqs []int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range seqs {
		m.published[s] = true
	}
	return nil
}

// Package relay moves events from the event store's outbox to the broker.
// Delivery is at-least-once: if the process dies after Publish but before
// MarkPublished, the batch is sent again and consumers must ignore duplicates.
package relay

import (
	"context"
	"log"
	"time"

	"github.com/Yang-Zhang1994/stockledger/internal/eventstore"
	"github.com/Yang-Zhang1994/stockledger/internal/inventory"
)

type Publisher interface {
	Publish(ctx context.Context, events []inventory.Event) error
}

type Relay struct {
	store eventstore.Store
	pub   Publisher
	batch int
}

func New(store eventstore.Store, pub Publisher) *Relay {
	return &Relay{store: store, pub: pub, batch: 100}
}

// Once publishes one batch and returns how many events it sent.
func (r *Relay) Once(ctx context.Context) (int, error) {
	recs, err := r.store.Unpublished(ctx, r.batch)
	if err != nil || len(recs) == 0 {
		return 0, err
	}
	events := make([]inventory.Event, len(recs))
	seqs := make([]int64, len(recs))
	for i, rec := range recs {
		events[i], seqs[i] = rec.Event, rec.Seq
	}
	if err := r.pub.Publish(ctx, events); err != nil {
		return 0, err
	}
	return len(recs), r.store.MarkPublished(ctx, seqs)
}

func (r *Relay) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, err := r.Once(ctx); err != nil {
				log.Printf("relay: %v (will retry)", err)
			}
		}
	}
}

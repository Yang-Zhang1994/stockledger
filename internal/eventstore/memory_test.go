package eventstore

import (
	"context"
	"errors"
	"testing"

	"github.com/Yang-Zhang1994/stockledger/internal/inventory"
)

func ev(v int) inventory.Event {
	return inventory.Event{SKU: "SKU-1", Version: v, Type: inventory.StockReceived, Qty: 1}
}

func TestAppendRejectsStaleExpectedVersion(t *testing.T) {
	ctx := context.Background()
	s := NewMemory()
	if err := s.Append(ctx, "SKU-1", 0, []inventory.Event{ev(1)}); err != nil {
		t.Fatal(err)
	}
	// A second writer that loaded the stream at version 0 must lose.
	if err := s.Append(ctx, "SKU-1", 0, []inventory.Event{ev(1)}); !errors.Is(err, ErrConflict) {
		t.Fatalf("want ErrConflict, got %v", err)
	}
}

func TestOutboxReturnsOnlyUnpublishedInOrder(t *testing.T) {
	ctx := context.Background()
	s := NewMemory()
	_ = s.Append(ctx, "SKU-1", 0, []inventory.Event{ev(1), ev(2)})
	_ = s.Append(ctx, "SKU-1", 2, []inventory.Event{ev(3)})

	recs, _ := s.Unpublished(ctx, 10)
	if len(recs) != 3 || recs[0].Seq != 1 || recs[2].Seq != 3 {
		t.Fatalf("unexpected outbox: %+v", recs)
	}
	_ = s.MarkPublished(ctx, []int64{1, 2})
	recs, _ = s.Unpublished(ctx, 10)
	if len(recs) != 1 || recs[0].Seq != 3 {
		t.Fatalf("after publish: %+v", recs)
	}
}

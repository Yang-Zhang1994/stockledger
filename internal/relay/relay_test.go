package relay

import (
	"context"
	"errors"
	"testing"

	"github.com/Yang-Zhang1994/stockledger/internal/eventstore"
	"github.com/Yang-Zhang1994/stockledger/internal/inventory"
)

type fakePublisher struct {
	sent []inventory.Event
	fail bool
}

func (f *fakePublisher) Publish(_ context.Context, evs []inventory.Event) error {
	if f.fail {
		return errors.New("broker down")
	}
	f.sent = append(f.sent, evs...)
	return nil
}

func seeded(t *testing.T) *eventstore.Memory {
	t.Helper()
	s := eventstore.NewMemory()
	err := s.Append(context.Background(), "SKU-1", 0, []inventory.Event{
		{SKU: "SKU-1", Version: 1, Type: inventory.StockReceived, Qty: 5},
		{SKU: "SKU-1", Version: 2, Type: inventory.StockReserved, OrderID: "o1", Qty: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRelayPublishesOnceThenDrains(t *testing.T) {
	ctx := context.Background()
	pub := &fakePublisher{}
	r := New(seeded(t), pub)

	if n, err := r.Once(ctx); err != nil || n != 2 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if n, _ := r.Once(ctx); n != 0 {
		t.Fatalf("second run re-sent %d events", n)
	}
	if len(pub.sent) != 2 || pub.sent[0].Version != 1 {
		t.Fatalf("sent %+v", pub.sent)
	}
}

func TestRelayKeepsEventsWhenBrokerIsDown(t *testing.T) {
	ctx := context.Background()
	store := seeded(t)
	pub := &fakePublisher{fail: true}
	r := New(store, pub)

	if _, err := r.Once(ctx); err == nil {
		t.Fatal("want error while broker is down")
	}
	pub.fail = false
	if n, err := r.Once(ctx); err != nil || n != 2 {
		t.Fatalf("after recovery n=%d err=%v", n, err)
	}
}

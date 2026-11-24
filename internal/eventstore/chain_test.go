package eventstore

import (
	"context"
	"testing"

	"github.com/Yang-Zhang1994/stockledger/internal/inventory"
)

// fourEvents stores a receive, two reservations and a commit for SKU-1 in
// separate appends, so the chain has to link across Append calls.
func fourEvents(t *testing.T) *Memory {
	t.Helper()
	ctx := context.Background()
	s := NewMemory()
	evs := []inventory.Event{
		{SKU: "SKU-1", Version: 1, Type: inventory.StockReceived, Qty: 10},
		{SKU: "SKU-1", Version: 2, Type: inventory.StockReserved, OrderID: "o1", Qty: 3},
		{SKU: "SKU-1", Version: 3, Type: inventory.StockReserved, OrderID: "o2", Qty: 2},
		{SKU: "SKU-1", Version: 4, Type: inventory.ReservationCommitted, OrderID: "o1", Qty: 3},
	}
	for i, e := range evs {
		if err := s.Append(ctx, "SKU-1", i, []inventory.Event{e}); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func verify(t *testing.T, s *Memory) Report {
	t.Helper()
	h, err := s.History(context.Background(), "SKU-1")
	if err != nil {
		t.Fatal(err)
	}
	return Verify("SKU-1", h)
}

func TestIntactChainVerifies(t *testing.T) {
	r := verify(t, fourEvents(t))
	if !r.OK || r.Events != 4 || len(r.Head) != 64 {
		t.Fatalf("want ok with a head hash, got %+v", r)
	}
}

func TestVerifyFindsTampering(t *testing.T) {
	cases := map[string]struct {
		tamper func(stream []Sealed) []Sealed
		at     int
	}{
		"edited quantity": {func(s []Sealed) []Sealed { s[1].Qty = 1; return s }, 2},
		"edited order id": {func(s []Sealed) []Sealed { s[2].OrderID = "o9"; return s }, 3},
		"deleted event":   {func(s []Sealed) []Sealed { return append(s[:1], s[2:]...) }, 2},
		"reordered events": {func(s []Sealed) []Sealed {
			s[1], s[2] = s[2], s[1]
			return s
		}, 2},
		// Rewriting an event and fixing its own hash still breaks the next link.
		"edited and rehashed": {func(s []Sealed) []Sealed {
			s[1].Qty = 1
			s[1].Hash = Hash(s[0].Hash, s[1].Event)
			return s
		}, 3},
		// Deleting an event and renumbering the rest breaks at the renumbered one.
		"deleted and renumbered": {func(s []Sealed) []Sealed {
			s = append(s[:1], s[2:]...)
			for i := range s {
				s[i].Version = i + 1
			}
			return s
		}, 2},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s := fourEvents(t)
			s.streams["SKU-1"] = c.tamper(s.streams["SKU-1"])
			r := verify(t, s)
			if r.OK || r.BrokenAt != c.at {
				t.Fatalf("want broken at version %d, got %+v", c.at, r)
			}
		})
	}
}

func TestTruncationChangesHead(t *testing.T) {
	s := fourEvents(t)
	before := verify(t, s).Head
	s.streams["SKU-1"] = s.streams["SKU-1"][:3]
	r := verify(t, s)
	// A shorter chain is still internally valid; only a recorded head reveals it.
	if !r.OK || r.Head == before {
		t.Fatalf("truncated chain should verify with a different head: %+v", r)
	}
}

func TestHashInputIsUnambiguous(t *testing.T) {
	a := inventory.Event{SKU: "SKU-1", Version: 2, Type: inventory.StockReserved, OrderID: "o1", Qty: 12}
	b := inventory.Event{SKU: "SKU-1", Version: 2, Type: inventory.StockReserved, OrderID: "o11", Qty: 2}
	if Hash("", a) == Hash("", b) {
		t.Fatal("different events must hash differently")
	}
}

package projection

import (
	"errors"
	"testing"

	"github.com/Yang-Zhang1994/stockledger/internal/inventory"
)

var history = []inventory.Event{
	{SKU: "SKU-1", Version: 1, Type: inventory.StockReceived, Qty: 10},
	{SKU: "SKU-1", Version: 2, Type: inventory.StockReserved, OrderID: "o1", Qty: 4},
	{SKU: "SKU-1", Version: 3, Type: inventory.ReservationCommitted, OrderID: "o1", Qty: 4},
}

func TestDuplicateDeliveryIsIgnored(t *testing.T) {
	p := NewAvailable()
	for _, e := range append(history, history...) { // whole stream delivered twice
		if err := p.Handle(e); err != nil {
			t.Fatal(err)
		}
	}
	if avail, v, _ := p.Get("SKU-1"); avail != 6 || v != 3 {
		t.Fatalf("available=%d version=%d", avail, v)
	}
}

func TestMissingEventIsReported(t *testing.T) {
	p := NewAvailable()
	_ = p.Handle(history[0])
	if err := p.Handle(history[2]); !errors.Is(err, inventory.ErrVersionGap) {
		t.Fatalf("want ErrVersionGap, got %v", err)
	}
}

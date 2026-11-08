//go:build integration

package eventstore

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	_ "github.com/go-sql-driver/mysql"

	"github.com/Yang-Zhang1994/stockledger/internal/inventory"
)

// Run with: docker compose up -d mysql && go test -tags integration ./...
func openMySQL(t *testing.T) *MySQL {
	t.Helper()
	dsn := os.Getenv("MYSQL_DSN")
	if dsn == "" {
		dsn = "stockledger:stockledger@tcp(127.0.0.1:3307)/stockledger?parseTime=true"
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`DELETE FROM events WHERE sku LIKE 'IT-%'`); err != nil {
		t.Fatalf("is MySQL up with migrations applied? %v", err)
	}
	return NewMySQL(db)
}

func TestMySQLUniqueVersionRejectsStaleWriter(t *testing.T) {
	ctx := context.Background()
	s := openMySQL(t)
	e := inventory.Event{SKU: "IT-1", Version: 1, Type: inventory.StockReceived, Qty: 5}
	if err := s.Append(ctx, "IT-1", 0, []inventory.Event{e}); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(ctx, "IT-1", 0, []inventory.Event{e}); !errors.Is(err, inventory.ErrConflict) {
		t.Fatalf("want conflict from duplicate key, got %v", err)
	}
	got, err := s.Load(ctx, "IT-1")
	if err != nil || len(got) != 1 || got[0].Qty != 5 {
		t.Fatalf("load: %+v %v", got, err)
	}
}

func TestMySQLOutboxRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := openMySQL(t)
	_ = s.Append(ctx, "IT-2", 0, []inventory.Event{
		{SKU: "IT-2", Version: 1, Type: inventory.StockReceived, Qty: 5},
		{SKU: "IT-2", Version: 2, Type: inventory.StockReserved, OrderID: "o1", Qty: 1},
	})
	recs, err := s.Unpublished(ctx, 1000)
	if err != nil {
		t.Fatal(err)
	}
	var seqs []int64
	for _, r := range recs {
		if r.Event.SKU == "IT-2" {
			seqs = append(seqs, r.Seq)
		}
	}
	if len(seqs) != 2 {
		t.Fatalf("want 2 unpublished IT-2 events, got %d", len(seqs))
	}
	if err := s.MarkPublished(ctx, seqs); err != nil {
		t.Fatal(err)
	}
	recs, _ = s.Unpublished(ctx, 1000)
	for _, r := range recs {
		if r.Event.SKU == "IT-2" {
			t.Fatalf("event %d still unpublished", r.Seq)
		}
	}
}

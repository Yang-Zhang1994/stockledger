package eventstore

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/go-sql-driver/mysql"

	"github.com/Yang-Zhang1994/stockledger/internal/inventory"
)

// MySQL stores events in one table. The UNIQUE (sku, version) key is what
// enforces optimistic concurrency: a stale writer hits a duplicate-key error.
// published_at doubles as the outbox flag, so saving an event and queueing it
// for Kafka happen in the same transaction.
type MySQL struct{ db *sql.DB }

func NewMySQL(db *sql.DB) *MySQL { return &MySQL{db: db} }

const mysqlDuplicateKey = 1062

func (s *MySQL) Load(ctx context.Context, sku string) ([]inventory.Event, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT version, type, order_id, qty FROM events WHERE sku = ? ORDER BY version`, sku)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []inventory.Event
	for rows.Next() {
		e := inventory.Event{SKU: sku}
		if err := rows.Scan(&e.Version, &e.Type, &e.OrderID, &e.Qty); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *MySQL) Append(ctx context.Context, sku string, expectedVersion int, events []inventory.Event) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for i, e := range events {
		if e.Version != expectedVersion+i+1 {
			return ErrConflict
		}
	}
	// The new events chain onto the hash of the event at expectedVersion. If a
	// concurrent writer appends first, the UNIQUE key still rejects this insert.
	prev := ""
	if expectedVersion > 0 {
		err := tx.QueryRowContext(ctx,
			`SELECT hash FROM events WHERE sku = ? AND version = ?`, sku, expectedVersion).Scan(&prev)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrConflict
		}
		if err != nil {
			return err
		}
	}
	for _, e := range seal(prev, events) {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO events (sku, version, type, order_id, qty, hash) VALUES (?, ?, ?, ?, ?, ?)`,
			sku, e.Version, e.Type, e.OrderID, e.Qty, e.Hash)
		var me *mysql.MySQLError
		if errors.As(err, &me) && me.Number == mysqlDuplicateKey {
			return ErrConflict
		}
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// History returns the SKU's events with their stored hashes, in version order.
func (s *MySQL) History(ctx context.Context, sku string) ([]Sealed, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT sku, version, type, order_id, qty, hash FROM events WHERE sku = ? ORDER BY version`, sku)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Sealed
	for rows.Next() {
		var e Sealed
		if err := rows.Scan(&e.SKU, &e.Version, &e.Type, &e.OrderID, &e.Qty, &e.Hash); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *MySQL) Unpublished(ctx context.Context, limit int) ([]Record, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT seq, sku, version, type, order_id, qty FROM events
		 WHERE published_at IS NULL ORDER BY seq LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Record
	for rows.Next() {
		var r Record
		if err := rows.Scan(&r.Seq, &r.Event.SKU, &r.Event.Version, &r.Event.Type, &r.Event.OrderID, &r.Event.Qty); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *MySQL) MarkPublished(ctx context.Context, seqs []int64) error {
	if len(seqs) == 0 {
		return nil
	}
	args := make([]any, len(seqs))
	for i, v := range seqs {
		args[i] = v
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE events SET published_at = NOW(3) WHERE seq IN (?`+strings.Repeat(",?", len(seqs)-1)+`)`, args...)
	return err
}

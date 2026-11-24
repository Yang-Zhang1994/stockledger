-- Hash chain per SKU: hash = SHA-256 over the previous event's hash and this
-- event's fields (see internal/eventstore/chain.go). Rows written before this
-- migration have an empty hash and will fail verification.
ALTER TABLE events ADD COLUMN hash CHAR(64) NOT NULL DEFAULT '' AFTER qty;

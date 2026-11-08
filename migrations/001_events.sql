-- Append-only event log. Rows are never updated except to stamp published_at,
-- which marks the event as handed to Kafka (the outbox flag).
CREATE TABLE IF NOT EXISTS events (
    seq          BIGINT AUTO_INCREMENT PRIMARY KEY,
    sku          VARCHAR(64)  NOT NULL,
    version      INT          NOT NULL,
    type         VARCHAR(32)  NOT NULL,
    order_id     VARCHAR(64)  NOT NULL DEFAULT '',
    qty          INT          NOT NULL,
    created_at   DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    published_at DATETIME(3)  NULL,
    UNIQUE KEY uq_stream_version (sku, version),
    KEY idx_unpublished (published_at, seq)
);

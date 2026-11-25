"""Reads live stock positions from the StockLedger HTTP API.

Demand history comes from the sales data; what is on the shelf right now comes
from StockLedger, the system of record, so a plan never trusts a stale export.
"""

from __future__ import annotations

import httpx


class StockLedger:
    def __init__(self, base_url: str = "http://localhost:8080", client: httpx.Client | None = None):
        self.http = client or httpx.Client(base_url=base_url, timeout=5.0)

    def position(self, sku: str) -> dict:
        """Units on hand and available (on hand minus reservations) for one SKU."""
        r = self.http.get(f"/skus/{sku}")
        r.raise_for_status()
        body = r.json()
        return {"on_hand": body["onHand"], "available": body["available"], "version": body["version"]}

    def positions(self, skus) -> dict:
        return {sku: self.position(sku) for sku in skus}

    def receive(self, sku: str, qty: int) -> None:
        self.http.post(f"/skus/{sku}/receive", json={"qty": qty}).raise_for_status()

    def reserve(self, sku: str, order_id: str, qty: int) -> None:
        self.http.post(f"/skus/{sku}/reservations", json={"orderId": order_id, "qty": qty}).raise_for_status()

import httpx

from replenish.stockledger import StockLedger


def test_reads_position_from_api():
    def handler(request):
        assert request.url.path == "/skus/BRK-PAD-220"
        return httpx.Response(200, json={"sku": "BRK-PAD-220", "onHand": 40, "available": 25, "version": 3})

    ledger = StockLedger(client=httpx.Client(base_url="http://sl", transport=httpx.MockTransport(handler)))
    assert ledger.position("BRK-PAD-220") == {"on_hand": 40, "available": 25, "version": 3}

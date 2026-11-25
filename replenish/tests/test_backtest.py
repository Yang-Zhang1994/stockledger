import pytest

from replenish.backtest import Data, evaluate, plan
from replenish.forecast import Boosted, MovingAverage


@pytest.fixture(scope="module")
def data():
    return Data.load()


@pytest.fixture(scope="module")
def results(data):
    return {"ma": evaluate(data, MovingAverage), "gb": evaluate(data, Boosted)}


def test_model_beats_baseline_on_holdout(results):
    ma, gb = results["ma"][0], results["gb"][0]
    assert gb.wape < ma.wape
    for fill in (0.98, 0.99):
        assert gb.stock_for_fill(fill) < ma.stock_for_fill(fill)


def test_more_safety_stock_never_lowers_fill(results):
    for res, _, _ in results.values():
        fills = [c[1] for c in res.curve]
        assert fills == sorted(fills)


def test_plan_orders_up_to_level_in_case_packs(data, results):
    _, sigma, method = results["gb"]
    rows = plan(data, method, sigma, {"BRK-PAD-220": {"available": 0, "on_order": 0}})
    row = next(r for r in rows if r["sku"] == "BRK-PAD-220")
    assert row["order_qty"] >= row["order_up_to"] and row["order_qty"] % row["case_pack"] == 0
    flush = plan(data, method, sigma, {"BRK-PAD-220": {"available": 10_000, "on_order": 0}})
    assert next(r for r in flush if r["sku"] == "BRK-PAD-220")["order_qty"] == 0

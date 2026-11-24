from replenish.policy import order_qty, safety_stock, simulate


def test_order_rounds_up_to_case_pack():
    assert order_qty(order_up_to=95, position=40, case_pack=12) == 60  # 55 short -> 5 cases
    assert order_qty(order_up_to=40, position=40, case_pack=12) == 0
    assert order_qty(order_up_to=30, position=40, case_pack=12) == 0


def test_safety_stock_grows_with_service_level():
    assert safety_stock(0.5, 10) == 0
    assert 16 < safety_stock(0.95, 10) < 17  # z = 1.645
    assert safety_stock(0.98, 10) > safety_stock(0.95, 10)


def test_order_arrives_after_lead_time_and_lost_sales_count():
    # Review on day 0 only; order up to 10 with 0 on hand, lead time 2.
    res = simulate([3, 3, 3, 3], [True, False, False, False], lambda i: 10, lead_time=2, case_pack=1,
                   start_on_hand=0)
    # Days 0-1: nothing on the shelf (6 lost). Day 2: 10 arrive, serve 3. Day 3: serve 3.
    assert res.demand == 12 and res.served == 6 and res.stockout_days == 2
    assert res.orders == 1 and res.avg_on_hand == (0 + 0 + 7 + 4) / 4


def test_position_counts_stock_on_order():
    # Two reviews before the first order lands: the second must not reorder.
    res = simulate([0, 0, 0, 0], [True, True, False, False], lambda i: 10, lead_time=3, case_pack=1,
                   start_on_hand=0)
    assert res.orders == 1

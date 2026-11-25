"""Weekly order-up-to replenishment, and a day-by-day simulation to score it.

Each Monday the planner orders enough to bring the inventory position (on hand
plus on order) up to S, the forecast for the lead time plus one review week,
plus safety stock. Orders are rounded up to the supplier's case pack and
arrive lead_time_days later. Demand that finds an empty shelf is lost.
"""

from __future__ import annotations

import math
from dataclasses import dataclass
from statistics import NormalDist

REVIEW_DAYS = 7


def safety_stock(service_level: float, sigma: float) -> float:
    """Units held to cover forecast error over the window at the given cycle service level."""
    return NormalDist().inv_cdf(service_level) * sigma


def order_qty(order_up_to: float, position: float, case_pack: int) -> int:
    """Units to order now, rounded up to whole cases; zero if the position already covers S."""
    short = order_up_to - position
    return 0 if short <= 0 else math.ceil(short / case_pack) * case_pack


@dataclass
class SimResult:
    demand: int = 0
    served: int = 0
    stockout_days: int = 0
    on_hand_sum: float = 0.0
    days: int = 0
    orders: int = 0

    @property
    def fill_rate(self) -> float:
        return self.served / self.demand if self.demand else 1.0

    @property
    def avg_on_hand(self) -> float:
        return self.on_hand_sum / self.days if self.days else 0.0


def simulate(demand: list[int], is_review: list[bool], order_up_to, lead_time: int, case_pack: int,
             start_on_hand: float) -> SimResult:
    """Run the policy over a demand path.

    order_up_to(i) returns S for review day i, computed from data up to day i-1,
    since the review happens at the start of the day before demand arrives.
    """
    res = SimResult()
    on_hand = start_on_hand
    arrivals: dict[int, int] = {}
    for i, d in enumerate(demand):
        on_hand += arrivals.pop(i, 0)
        if is_review[i]:
            position = on_hand + sum(arrivals.values())
            q = order_qty(order_up_to(i), position, case_pack)
            if q:
                arrivals[i + lead_time] = arrivals.get(i + lead_time, 0) + q
                res.orders += 1
        served = min(on_hand, d)
        on_hand -= served
        res.demand += d
        res.served += int(served)
        res.stockout_days += int(served < d)
        res.on_hand_sum += on_hand
        res.days += 1
    return res

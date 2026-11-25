"""Compare the two forecasters on held-out months, then plan the next order.

Timeline (all simulated):
  2024-01-01 .. 2026-01-31  fit a first model
  2026-02-01 .. 2026-04-30  measure its errors -> safety stock sigma per SKU
  refit on everything up to 2026-04-30
  2026-05-04 .. 2026-10-31  holdout: forecast accuracy and a simulated season
                            of weekly ordering, scored on fill rate and stock held
"""

from __future__ import annotations

from dataclasses import dataclass, field

import numpy as np
import pandas as pd

from .forecast import Boosted, MovingAverage
from .policy import REVIEW_DAYS, order_qty, safety_stock, simulate
from .simulate import demand, sku_table

SIGMA_FIT_END, TRAIN_END, HOLDOUT_START = "2026-01-31", "2026-04-30", "2026-05-04"


@dataclass
class Data:
    skus: pd.DataFrame
    history: dict  # sku -> (units ndarray, dates DatetimeIndex)
    horizons: dict  # sku -> lead time + review period

    @classmethod
    def load(cls, frame: pd.DataFrame | None = None) -> "Data":
        frame = demand() if frame is None else frame
        skus = sku_table().set_index("sku")
        history = {}
        for sku, g in frame.sort_values("date").groupby("sku"):
            history[sku] = (g["units"].to_numpy(), pd.DatetimeIndex(g["date"]))
        horizons = {s: int(skus.loc[s, "lead_time_days"]) + REVIEW_DAYS for s in history}
        return cls(skus, history, horizons)

    def day(self, date: str) -> int:
        dates = next(iter(self.history.values()))[1]
        return int(dates.get_loc(pd.Timestamp(date)))


def _errors(method, data: Data, first: int, last: int) -> dict:
    """Out-of-sample errors of horizon totals for weekly forecast dates in [first, last]."""
    out = {}
    for sku, (units, dates) in data.history.items():
        h = data.horizons[sku]
        errs, actual = [], []
        for t in range(first, last - h + 1, 7):
            fc = method.predict_daily(units, dates, t, h) * h
            y = units[t + 1 : t + 1 + h].sum()
            errs.append(y - fc)
            actual.append(y)
        out[sku] = (np.array(errs), np.array(actual))
    return out


# Safety stock multipliers swept to trace stock held against fill rate, so the
# methods can be compared at equal service rather than at their own settings.
MULTIPLIERS = [0.0, 0.25, 0.5, 0.75, 1.0, 1.25, 1.5, 2.0, 2.5, 3.0]


@dataclass
class MethodResult:
    name: str
    wape: float
    curve: list  # (multiplier, fill rate, avg units on hand, stockout days), all SKUs together
    per_sku: dict = field(default_factory=dict)

    def at(self, multiplier: float) -> tuple:
        return next(c for c in self.curve if c[0] == multiplier)

    def stock_for_fill(self, target: float) -> float:
        """Units on hand needed for the target fill rate, interpolated along the curve."""
        pts = sorted((c[1], c[2]) for c in self.curve)
        fills, stock = [p[0] for p in pts], [p[1] for p in pts]
        if not fills[0] <= target <= fills[-1]:
            return float("nan")
        return float(np.interp(target, fills, stock))


def evaluate(data: Data, make_method) -> tuple[MethodResult, dict, object]:
    sigma_end, train_end, hold = data.day(SIGMA_FIT_END), data.day(TRAIN_END), data.day(HOLDOUT_START)
    last = len(next(iter(data.history.values()))[0]) - 1

    first = make_method().fit(data.history, data.horizons, sigma_end)
    sigma = {s: float(e.std(ddof=1)) for s, (e, _) in _errors(first, data, sigma_end, train_end).items()}

    method = make_method().fit(data.history, data.horizons, train_end)
    holdout = _errors(method, data, hold - 1, last)
    abs_err = sum(np.abs(e).sum() for e, _ in holdout.values())
    total = sum(a.sum() for _, a in holdout.values())

    totals = {k: [0, 0, 0.0, 0] for k in MULTIPLIERS}  # served, demand, on hand, stockout days
    per_sku = {}
    for sku, (units, dates) in data.history.items():
        row = data.skus.loc[sku]
        h, lt, pack = data.horizons[sku], int(row.lead_time_days), int(row.case_pack)
        ss = safety_stock(float(row.service_level), sigma[sku])
        path = list(units[hold:])
        reviews = [d.day_of_week == 0 for d in dates[hold:]]
        # Forecast once per review day; S on day i uses data through day i-1.
        fc = {i: method.predict_daily(units, dates, hold + i - 1, h) * h for i, r in enumerate(reviews) if r or i == 0}
        sims = {}
        for k in MULTIPLIERS:
            def s_at(i, k=k):
                return fc[i] + k * ss
            sims[k] = simulate(path, reviews, s_at, lt, pack, start_on_hand=s_at(0))
            t = totals[k]
            t[0] += sims[k].served
            t[1] += sims[k].demand
            t[2] += sims[k].avg_on_hand
            t[3] += sims[k].stockout_days
        e, a = holdout[sku]
        per_sku[sku] = {
            "wape": float(np.abs(e).sum() / a.sum()) if a.sum() else float("nan"),
            "fill_rate": sims[1.0].fill_rate,
            "avg_on_hand": sims[1.0].avg_on_hand,
            "stockout_days": sims[1.0].stockout_days,
            "sigma": sigma[sku],
            "safety_stock": ss,
        }
    curve = [(k, t[0] / t[1], t[2], t[3]) for k, t in totals.items()]
    return MethodResult(method.name, abs_err / total, curve, per_sku), sigma, method


def run(data: Data | None = None) -> dict:
    data = data or Data.load()
    return {key: evaluate(data, make)[0] for key, make in {"moving_average": MovingAverage, "boosted": Boosted}.items()}


def plan(data: Data, method, sigma: dict, positions: dict) -> list[dict]:
    """Next order per SKU, forecasting from the last day of history.

    positions: sku -> units available now (from StockLedger) and on order.
    """
    rows = []
    for sku, (units, dates) in data.history.items():
        row = data.skus.loc[sku]
        h, pack = data.horizons[sku], int(row.case_pack)
        t = len(units) - 1
        daily = method.predict_daily(units, dates, t, h)
        ss = safety_stock(float(row.service_level), sigma[sku])
        s = daily * h + ss
        pos = positions.get(sku, {"available": 0, "on_order": 0})
        position = pos["available"] + pos.get("on_order", 0)
        rows.append({
            "sku": sku,
            "description": row.description,
            "available": pos["available"],
            "on_order": pos.get("on_order", 0),
            "forecast_per_day": round(daily, 2),
            "window_days": h,
            "safety_stock": round(ss, 1),
            "order_up_to": round(s, 1),
            "order_qty": order_qty(s, position, pack),
            "case_pack": pack,
            "days_of_cover": round(pos["available"] / daily, 1) if daily > 0 else None,
            "as_of": str(dates[t].date()),
        })
    return rows

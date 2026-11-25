"""Forecast a SKU's demand over the next H days from its daily history.

Two methods share one interface, so the backtest can compare them fairly:

- MovingAverage: the last 28 days' mean, the usual spreadsheet baseline.
- Boosted: a gradient-boosted model trained across all SKUs. It predicts the
  coming window's mean as a ratio to the SKU's recent level, which lets one
  model serve a 30-a-day consumable and a part used twice a month.

Every feature for a forecast made on day t uses days up to and including t
only; test_forecast.py checks that changing later days changes nothing.
"""

from __future__ import annotations

import numpy as np
import pandas as pd
from sklearn.ensemble import HistGradientBoostingRegressor

FEATURES = ["r7", "r28", "yoy", "season", "zero91", "doy_sin", "doy_cos", "horizon"]
EPS = 0.05  # units/day; keeps ratios finite for parts with no recent demand


def _window_mean(csum: np.ndarray, end: int, n: int) -> float:
    """Mean of x[end-n+1 .. end] given csum = cumsum with a leading 0."""
    start = max(end - n + 1, 0)
    return (csum[end + 1] - csum[start]) / (end + 1 - start)


def features(units: np.ndarray, dates: pd.DatetimeIndex, t: int, horizon: int) -> dict:
    """Features for a forecast made at the end of day index t, covering days t+1..t+horizon."""
    csum = np.concatenate([[0.0], np.cumsum(units, dtype=float)])
    level = _window_mean(csum, t, 91) + EPS
    # Same window a year earlier, which is entirely in the past when horizon < 364.
    ly_end = t + horizon - 364
    has_ly = ly_end - horizon - 90 >= 0
    yoy = _window_mean(csum, ly_end, horizon) / level if has_ly else np.nan
    # How demand moved at this time last year: the window against the 91 days
    # before it. This is what tells the model winter parts are about to drop.
    ly_level = _window_mean(csum, ly_end - horizon, 91) + EPS if has_ly else np.nan
    season = (_window_mean(csum, ly_end, horizon) + EPS) / ly_level if has_ly else np.nan
    mid = dates[t] + pd.Timedelta(days=(horizon + 1) / 2)
    angle = 2 * np.pi * mid.day_of_year / 365.25
    recent = units[max(t - 90, 0) : t + 1]
    return {
        "level": level,
        "r7": (_window_mean(csum, t, 7) + EPS) / level,
        "r28": (_window_mean(csum, t, 28) + EPS) / level,
        "yoy": yoy,
        "season": season,
        "zero91": float(np.mean(recent == 0)),
        "doy_sin": np.sin(angle),
        "doy_cos": np.cos(angle),
        "horizon": horizon,
    }


def training_rows(units: np.ndarray, dates: pd.DatetimeIndex, horizon: int, last_day: int) -> pd.DataFrame:
    """One row per forecast date whose whole target window ends by last_day."""
    rows = []
    for t in range(90, last_day - horizon + 1):
        f = features(units, dates, t, horizon)
        f["target"] = units[t + 1 : t + 1 + horizon].mean()
        rows.append(f)
    return pd.DataFrame(rows)


class MovingAverage:
    name = "28-day moving average"

    def fit(self, history: dict, horizons: dict, last_day: int) -> "MovingAverage":
        return self

    def predict_daily(self, units: np.ndarray, dates: pd.DatetimeIndex, t: int, horizon: int) -> float:
        return float(units[max(t - 27, 0) : t + 1].mean())


class Boosted:
    name = "gradient-boosted model"

    def __init__(self, seed: int = 0):
        self.model = HistGradientBoostingRegressor(
            max_iter=300, learning_rate=0.05, max_leaf_nodes=15, min_samples_leaf=40, random_state=seed
        )

    def fit(self, history: dict, horizons: dict, last_day: int) -> "Boosted":
        """history: sku -> (units, dates); horizons: sku -> forecast horizon in days."""
        frames = [training_rows(u, d, horizons[sku], last_day) for sku, (u, d) in history.items()]
        rows = pd.concat(frames, ignore_index=True)
        self.model.fit(rows[FEATURES], rows["target"] / rows["level"])
        return self

    def predict_daily(self, units: np.ndarray, dates: pd.DatetimeIndex, t: int, horizon: int) -> float:
        f = features(units, dates, t, horizon)
        ratio = self.model.predict(pd.DataFrame([f])[FEATURES])[0]
        return float(max(ratio, 0.0) * f["level"])

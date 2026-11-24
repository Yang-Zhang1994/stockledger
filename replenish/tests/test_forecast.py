import numpy as np
import pandas as pd

from replenish.forecast import Boosted, MovingAverage, features
from replenish.simulate import demand


def series(sku="BRK-PAD-220"):
    d = demand()
    g = d[d.sku == sku].sort_values("date")
    return g["units"].to_numpy(), pd.DatetimeIndex(g["date"])


def test_simulation_is_deterministic():
    assert demand().equals(demand())


def test_features_ignore_the_future():
    units, dates = series()
    t, h = 500, 21
    before = features(units, dates, t, h)
    changed = units.copy()
    changed[t + 1 :] = 10_000  # the forecast window and everything after it
    after = features(changed, dates, t, h)
    assert before == after or all(
        (np.isnan(before[k]) and np.isnan(after[k])) or before[k] == after[k] for k in before
    )


def test_moving_average_is_last_28_days():
    units = np.arange(100)
    dates = pd.date_range("2025-01-01", periods=100)
    assert MovingAverage().predict_daily(units, dates, 99, 14) == np.mean(np.arange(72, 100))


def test_boosted_tracks_a_seasonal_drop():
    # De-icing fluid in late March: demand is about to fall. The moving average
    # lags behind; the model should forecast lower.
    units, dates = series("DE-ICE-20L")
    t = int(dates.get_loc(pd.Timestamp("2026-03-20")))
    h = 14
    model = Boosted().fit({"DE-ICE-20L": (units, dates)}, {"DE-ICE-20L": h}, t)
    actual = units[t + 1 : t + 1 + h].mean()
    ma = MovingAverage().predict_daily(units, dates, t, h)
    gb = model.predict_daily(units, dates, t, h)
    assert gb < ma
    assert abs(gb - actual) < abs(ma - actual)

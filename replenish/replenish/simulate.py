"""Simulated daily demand for a rail maintenance depot's spare parts.

There is no real sales history to forecast, so this generates one with the
patterns spare-parts planners deal with: steady consumables, weekday-heavy
work, winter peaks, slow growth, and intermittent parts that sit at zero for
weeks. The seed is fixed so every run and test sees the same history.
"""

from __future__ import annotations

import numpy as np
import pandas as pd

START, END = "2024-01-01", "2026-10-31"

# sku, description, base units/day, pattern, lead time (days), case pack, service level
SKUS = [
    ("BRK-PAD-220", "Brake pad set", 18.0, "weekday", 14, 10, 0.95),
    ("BRK-DSC-410", "Brake disc", 4.0, "weekday", 21, 2, 0.95),
    ("WHL-BRG-118", "Wheel bearing", 6.0, "steady", 21, 4, 0.98),
    ("PNT-HTR-050", "Point heater element", 3.0, "winter", 14, 5, 0.98),
    ("DE-ICE-20L", "De-icing fluid 20 L", 25.0, "winter", 7, 12, 0.95),
    ("WPR-BLD-900", "Wiper blade", 9.0, "winter", 7, 20, 0.90),
    ("FLT-HVAC-07", "HVAC filter", 12.0, "summer", 10, 24, 0.95),
    ("LED-HDL-24V", "LED headlight module", 1.5, "growth", 14, 2, 0.95),
    ("CBL-TIE-300", "Cable ties (bag)", 30.0, "steady", 7, 50, 0.90),
    ("DOOR-SNS-02", "Door sensor", 0.25, "intermittent", 28, 1, 0.98),
    ("PANTO-STRP", "Pantograph carbon strip", 0.15, "intermittent", 35, 2, 0.98),
    ("CPL-HOSE-12", "Coupler air hose", 2.5, "growth", 14, 4, 0.95),
]
COLUMNS = ["sku", "description", "base", "pattern", "lead_time_days", "case_pack", "service_level"]


def sku_table() -> pd.DataFrame:
    return pd.DataFrame(SKUS, columns=COLUMNS)


def _rate(pattern: str, base: float, days: pd.DatetimeIndex) -> np.ndarray:
    t = np.arange(len(days))
    doy = days.day_of_year.to_numpy()
    weekday = days.day_of_week.to_numpy() < 5
    winter = 1 + 0.9 * np.cos(2 * np.pi * (doy - 15) / 365.25)  # peaks mid-January
    rate = np.full(len(days), base)
    if pattern == "weekday":
        rate = np.where(weekday, base * 1.25, base * 0.4)
    elif pattern == "winter":
        rate = base * np.clip(winter, 0.15, None)
    elif pattern == "summer":
        rate = base * (1 + 0.6 * np.cos(2 * np.pi * (doy - 200) / 365.25))
    elif pattern == "growth":
        rate = base * (1 + 1.2 * t / len(days)) * np.where(weekday, 1.1, 0.75)
    return rate


def demand(seed: int = 7) -> pd.DataFrame:
    """Daily units demanded per SKU: columns date, sku, units."""
    rng = np.random.default_rng(seed)
    days = pd.date_range(START, END, freq="D")
    frames = []
    for sku, _, base, pattern, *_ in SKUS:
        if pattern == "intermittent":
            # A part fails on a few days a month; each failure needs 1-3 units
            # (2 on average), so failing on base / 2 of days averages `base` a day.
            hits = rng.random(len(days)) < base / 2
            units = np.where(hits, rng.integers(1, 4, len(days)), 0)
        else:
            rate = _rate(pattern, base, days)
            # Gamma-Poisson mixing gives the extra variance real demand has.
            units = rng.poisson(rng.gamma(shape=8.0, scale=rate / 8.0))
        frames.append(pd.DataFrame({"date": days, "sku": sku, "units": units.astype(int)}))
    return pd.concat(frames, ignore_index=True)

"""Command line: python -m replenish {backtest,seed,plan,ask}.

  backtest            compare the forecasters and write results/backtest.md
  seed  [--url]       stock a running StockLedger with every part (demo data)
  plan  [--url]       this week's orders, using live stock from StockLedger
  ask "question"      ask about the plan in plain English (needs ANTHROPIC_API_KEY)
"""

from __future__ import annotations

import argparse
import math
from pathlib import Path

from .assistant import Tools, ask
from .backtest import Boosted, Data, evaluate, plan, run
from .report import write_report
from .stockledger import StockLedger

RESULTS = Path(__file__).resolve().parents[1] / "results" / "backtest.md"


def build(url: str):
    """Plan rows and per-part details from the boosted model and live StockLedger positions."""
    data = Data.load()
    result, sigma, method = evaluate(data, Boosted)
    positions = StockLedger(url).positions(data.history)
    rows = plan(data, method, sigma, positions)
    details = {}
    for row in rows:
        units, dates = data.history[row["sku"]]
        part = data.skus.loc[row["sku"]]
        weekly = [int(units[i : i + 7].sum()) for i in range(len(units) - 56, len(units), 7)]
        details[row["sku"]] = {
            **row,
            "lead_time_days": int(part.lead_time_days),
            "service_level": float(part.service_level),
            "backtest_wape": round(result.per_sku[row["sku"]]["wape"], 3),
            "last_8_weeks_demand": weekly,
        }
    return rows, details


def seed(url: str) -> None:
    """Gives each part about 1-4 weeks of stock and a few open reservations."""
    data, ledger = Data.load(), StockLedger(url)
    for n, (sku, (units, _)) in enumerate(sorted(data.history.items())):
        daily = units[-28:].mean()
        ledger.receive(sku, max(2, math.ceil(daily * (7 + 3 * n))))
        if daily >= 1:
            ledger.reserve(sku, f"wo-{1000 + n}", max(1, round(daily)))


def main() -> None:
    p = argparse.ArgumentParser(prog="replenish")
    sub = p.add_subparsers(dest="cmd", required=True)
    sub.add_parser("backtest")
    for name in ("seed", "plan"):
        sub.add_parser(name).add_argument("--url", default="http://localhost:8080")
    a = sub.add_parser("ask")
    a.add_argument("question")
    a.add_argument("--url", default="http://localhost:8080")
    args = p.parse_args()

    if args.cmd == "backtest":
        print(write_report(run(), RESULTS))
    elif args.cmd == "seed":
        seed(args.url)
        print("seeded", args.url)
    elif args.cmd == "plan":
        rows, _ = build(args.url)
        cols = ["sku", "available", "forecast_per_day", "days_of_cover", "order_up_to", "order_qty"]
        print("  ".join(f"{c:>16}" for c in cols))
        for r in rows:
            print("  ".join(f"{str(r[c]):>16}" for c in cols))
    elif args.cmd == "ask":
        rows, details = build(args.url)
        print(ask(args.question, Tools(rows, details)))


if __name__ == "__main__":
    main()

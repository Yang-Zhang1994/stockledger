"""Writes the backtest comparison as Markdown."""

from __future__ import annotations

from pathlib import Path

FILLS = (0.98, 0.99, 0.995)


def write_report(results: dict, path: Path) -> str:
    base, model = results["moving_average"], results["boosted"]
    lines = [
        "# Backtest: 28-day moving average vs gradient-boosted model",
        "",
        "Simulated demand for 12 rail spare parts, May 4 – Oct 31, 2026 (held out from training).",
        "Weekly order-up-to policy, lost sales, each part's own lead time and case pack.",
        "",
        "## Forecast error over each part's lead time + 1 week",
        "",
        "| Method | WAPE |",
        "|---|---|",
        f"| {base.name} | {base.wape:.1%} |",
        f"| {model.name} | {model.wape:.1%} |",
        "",
        "## Stock needed for the same fill rate (units on hand, all parts)",
        "",
        "| Fill rate | " + " | ".join(r.name for r in (base, model)) + " | Change |",
        "|---|---|---|---|",
    ]
    for f in FILLS:
        b, m = base.stock_for_fill(f), model.stock_for_fill(f)
        lines.append(f"| {f:.1%} | {b:.0f} | {m:.0f} | {m / b - 1:+.0%} |")
    lines += [
        "",
        "## At each part's configured service level",
        "",
        "| Method | Fill rate | Avg units on hand | Stockout days |",
        "|---|---|---|---|",
    ]
    for r in (base, model):
        _, fill, stock, outs = r.at(1.0)
        lines.append(f"| {r.name} | {fill:.2%} | {stock:.0f} | {outs} |")
    lines += ["", "## Per part", "", "| Part | WAPE (MA) | WAPE (model) | Fill (MA) | Fill (model) |", "|---|---|---|---|---|"]
    for sku in sorted(base.per_sku):
        b, m = base.per_sku[sku], model.per_sku[sku]
        lines.append(f"| {sku} | {b['wape']:.1%} | {m['wape']:.1%} | {b['fill_rate']:.1%} | {m['fill_rate']:.1%} |")
    text = "\n".join(lines) + "\n"
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(text)
    return text

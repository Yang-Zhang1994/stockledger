# Backtest: 28-day moving average vs gradient-boosted model

Simulated demand for 12 rail spare parts, May 4 – Oct 31, 2026 (held out from training).
Weekly order-up-to policy, lost sales, each part's own lead time and case pack.

## Forecast error over each part's lead time + 1 week

| Method | WAPE |
|---|---|
| 28-day moving average | 17.1% |
| gradient-boosted model | 11.8% |

## Stock needed for the same fill rate (units on hand, all parts)

| Fill rate | 28-day moving average | gradient-boosted model | Change |
|---|---|---|---|
| 98.0% | 867 | 728 | -16% |
| 99.0% | 1063 | 878 | -17% |
| 99.5% | 1406 | 1005 | -29% |

## At each part's configured service level

| Method | Fill rate | Avg units on hand | Stockout days |
|---|---|---|---|
| 28-day moving average | 98.69% | 955 | 36 |
| gradient-boosted model | 99.19% | 914 | 30 |

## Per part

| Part | WAPE (MA) | WAPE (model) | Fill (MA) | Fill (model) |
|---|---|---|---|---|
| BRK-DSC-410 | 9.6% | 9.1% | 99.7% | 99.2% |
| BRK-PAD-220 | 9.4% | 8.9% | 99.6% | 99.4% |
| CBL-TIE-300 | 12.9% | 11.9% | 99.4% | 99.5% |
| CPL-HOSE-12 | 10.2% | 10.4% | 100.0% | 100.0% |
| DE-ICE-20L | 44.4% | 11.6% | 100.0% | 100.0% |
| DOOR-SNS-02 | 70.9% | 53.2% | 100.0% | 87.5% |
| FLT-HVAC-07 | 15.7% | 13.9% | 97.4% | 98.8% |
| LED-HDL-24V | 16.9% | 15.5% | 95.3% | 98.5% |
| PANTO-STRP | 96.6% | 76.0% | 100.0% | 100.0% |
| PNT-HTR-050 | 58.7% | 23.1% | 100.0% | 100.0% |
| WHL-BRG-118 | 12.6% | 7.2% | 92.7% | 95.8% |
| WPR-BLD-900 | 48.2% | 22.7% | 99.7% | 100.0% |

# Regime Sleeve

Cross-asset rotation for Bitget AI Base Camp S2, Alpha Factory, sub-theme **Cross-Asset Allocation / Rotation**.

The book holds one sleeve at a time: Bitget Reality **rSPYUSDT** (the broad US market), **BTCUSDT**, **PAXGUSDT** (tokenized gold), or cash. Rules are fixed in `internal/strategy/spec.go`.

## The rule (frozen)

Read yesterday's close only; today's close is the result, not an input.

- **Risk-on:** hold **rSPY** when it is above its 100-day average and 20-day realized vol is under 28% a year.
- **Risk-off, still calm:** hold **BTC** if it is above its 50-day average.
- **Otherwise:** hold **PAXG** if it is above its 50-day average, else **cash**.
- **Hysteresis:** a sleeve must clear its trend line by **3%** to switch on and only drops when price falls back below the line, and it is held at least **two trading weeks**. That buffer holds turnover near **three switches a year**.
- Each switch is charged **15 bps** (10 bps fee + 5 bps slippage); a **40 bps** harsh-cost run is reported alongside.

An earlier version chased the strongest S&P sector token in risk-on. It overfit in sample and its out-of-sample Sharpe went negative, so it was dropped in favour of holding the broad market. `Spec.SectorChase()` keeps that discarded rule for comparison. Qwen, when `QWEN_API_KEY` is set, only writes the sentence under the current signal; it does not pick the sleeve.

## Data and provenance

Prices come from `bitget-mcp-server` (`BITGET_US_MCP_URL`, handbook endpoint `https://agent.bitget.com/mcp`, tool `do_query` / `crypto/spot/kline`). If that series is short, the same Bitget symbol is loaded from the public spot candle API and the page says so. Yahoo Finance is not used.

Bitget listed the rToken US-stock pairs (rSPYUSDT and the sector tokens) on **2026-06-02** (Reality / Stocks 2.0). The candles Bitget serves before that day are the **underlying reference price** for the same stock/ETF, not live rToken trades; candles on and after it are **genuine rToken market prints**. The code labels this on every rToken series and in `internal/market/provenance.go`. Because the out-of-sample window is the last ~63 weekdays (starting late June 2026), **the entire held-out test runs on live rToken data.** BTCUSDT and PAXGUSDT are ordinary spot pairs, live throughout.

Weekend candles are not treated as fills. A position held on Friday is marked through Monday's close.

## Run

From this directory, with `.env` present:

```bash
go run ./cmd/server
```

Open the URL it prints (port comes from `PORT`, default 8080). The first load pages several years of daily candles and then caches them in `.cache/` for six hours. Refresh prices on the page to bypass the cache.

```bash
go test ./...
go run ./cmd/server -once
```

`-once` prints the backtest figures and exits. `-fresh` ignores the cache.

## Docker / deploy

The image is a static, stripped binary on Alpine (templates and static assets are compiled in via `go:embed`). No external Go modules.

```bash
# build and run with compose (recommended)
docker compose up --build -d
# → http://localhost:8080   (set HOST_PORT to change the host port)

# or plain docker
docker build -t regime-sleeve .
docker run -d -p 8080:8080 -v regime-cache:/app/.cache --name regime-sleeve regime-sleeve
```

- The container always listens on **8080** inside; map it to any host port (`HOST_PORT` in compose, or `-p`).
- `GET /health` returns 200 without triggering a price download and backs the container `HEALTHCHECK`.
- The price cache is written to `/app/.cache`; the named volume `regime-cache` keeps it across restarts.
- Config comes from environment variables (`PORT`, `BITGET_US_MCP_URL`, optional `QWEN_API_KEY`, ...). Compose reads `.env` if present; `.env` is never baked into the image (see `.dockerignore`).
- Runs as a non-root user (uid 10001).

## What the page is for

Judges need strategy code, a backtest of at least 60 days, and at least 30 days held out of sample. The page shows Sharpe, Sortino, max drawdown, turnover, the in-sample versus out-of-sample split, and the handbook decay check (out-of-sample Sharpe below half the in-sample Sharpe). `GET /backtest.csv` is the daily path.

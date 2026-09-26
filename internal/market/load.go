package market

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"time"

	"regime-sleeve/internal/mcp"
	"regime-sleeve/internal/strategy"
)

const (
	lookback = 5 * 365 * 24 * time.Hour
)

// Load pulls the US sector tokens plus Bitcoin and gold.
// History comes from bitget-mcp-server (crypto/spot/kline). If a series
// comes back short, the same symbol is filled from the public Bitget spot
// candle API and the source line says so. A missing sector does not fail
// the book; the market, Bitcoin, and gold series are required.
func Load(ctx context.Context, mcpURL string) (Book, error) {
	since := time.Now().UTC().Add(-lookback)
	listings := strategy.Listings()
	type fetched struct {
		key  string
		bars []Bar
		meta SeriesMeta
		err  error
	}
	out := make([]fetched, len(listings))
	errCh := make(chan int, len(listings))
	// Cap concurrent Bitget requests. Firing all 14 symbols (each paginated)
	// at once trips the public candle API's rate limit and drops series at
	// random, which can fail a required leg. A small pool keeps it reliable.
	sem := make(chan struct{}, 4)
	for i, item := range listings {
		go func(i int, item strategy.Listing) {
			defer func() { errCh <- i }()
			sem <- struct{}{}
			defer func() { <-sem }()
			required := item.Role != "sector"
			// Price series come from Bitget's public spot candle API. Per the S2
			// handbook, bitget-mcp-server serves US-stock quotes and fundamentals,
			// not these rToken/crypto spot klines — its kline entry returns no
			// usable data for these pairs — so it is probed once for the record
			// (see probeEquityHistorical) rather than driving the price feed.
			bars, err := bitgetSpotRetry(ctx, item.Pair, since, required)
			if err != nil || len(bars) == 0 {
				if required {
					if err == nil {
						err = fmt.Errorf("no candles")
					}
					out[i] = fetched{err: fmt.Errorf("%s: %w", item.Pair, err)}
					return
				}
				log.Printf("%s: no usable history, left out of the rotation", item.Title)
				out[i] = fetched{key: item.Key, meta: metaFor(item.Title, item.Pair, "unavailable", nil, "no daily history returned")}
				return
			}
			out[i] = fetched{key: item.Key, bars: bars, meta: metaFor(item.Title, item.Pair, "Bitget spot candle API (api.bitget.com)", bars, "")}
		}(i, item)
	}
	for range listings {
		<-errCh
	}
	var market, btc, gold []Bar
	sectors := map[string][]Bar{}
	meta := make([]SeriesMeta, 0, len(out))
	for i, item := range listings {
		f := out[i]
		if f.err != nil {
			return Book{}, f.err
		}
		meta = append(meta, f.meta)
		switch item.Role {
		case "market":
			market = f.bars
		case "crypto":
			btc = f.bars
		case "commodity":
			gold = f.bars
		default:
			if len(f.bars) > 0 {
				sectors[item.Key] = f.bars
			}
		}
	}
	hist := probeEquityHistorical(ctx, mcp.New(mcpURL))
	days := align(market, btc, gold, sectors)
	book := Book{
		Days:     days,
		Meta:     labelProvenance(meta),
		HistNote: hist,
		Prov:     provenanceOf(days),
		Fetched:  time.Now().UTC(),
	}
	if len(book.Days) < 160 {
		return Book{}, fmt.Errorf("only %d aligned weekdays after the join; need history for a 100-day average plus a 60-day test", len(book.Days))
	}
	return book, nil
}

func metaFor(name, symbol, source string, bars []Bar, note string) SeriesMeta {
	m := SeriesMeta{Name: name, Symbol: symbol, Source: source, Bars: len(bars), Note: note}
	if len(bars) > 0 {
		m.Start = bars[0].Date
		m.End = bars[len(bars)-1].Date
	}
	return m
}

func bitgetSpot(ctx context.Context, symbol string, since time.Time) ([]Bar, error) {
	var all []Bar
	var end *int64
	client := &http.Client{Timeout: 20 * time.Second}
	for page := 0; page < 40; page++ {
		url := fmt.Sprintf("https://api.bitget.com/api/v2/spot/market/candles?symbol=%s&granularity=1day&limit=200", symbol)
		if end != nil {
			url += fmt.Sprintf("&endTime=%d", *end)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		var payload struct {
			Code string     `json:"code"`
			Msg  string     `json:"msg"`
			Data [][]string `json:"data"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, err
		}
		if payload.Code != "00000" {
			return nil, fmt.Errorf("bitget %s: %s", symbol, payload.Msg)
		}
		if len(payload.Data) == 0 {
			break
		}
		var oldest int64
		for _, row := range payload.Data {
			if len(row) < 6 {
				continue
			}
			ts, err := parseInt64(row[0])
			if err != nil {
				continue
			}
			open, _ := parseFloat(row[1])
			high, _ := parseFloat(row[2])
			low, _ := parseFloat(row[3])
			closePx, err := parseFloat(row[4])
			if err != nil || closePx <= 0 {
				continue
			}
			vol, _ := parseFloat(row[5])
			when := time.UnixMilli(ts).UTC()
			all = append(all, Bar{
				Date:   dateOnly(when),
				Open:   open,
				High:   high,
				Low:    low,
				Close:  closePx,
				Volume: vol,
			})
			if oldest == 0 || ts < oldest {
				oldest = ts
			}
		}
		if !time.UnixMilli(oldest).After(since) || len(payload.Data) < 200 {
			break
		}
		next := oldest - 1
		if end != nil && next >= *end {
			break
		}
		end = &next
	}
	return clean(all, since), nil
}

// bitgetSpotRetry wraps bitgetSpot with backoff so a transient rate-limit or
// empty page does not drop a series. Required legs (rSPY, BTC, gold) get more
// attempts because a single miss there fails the whole load.
func bitgetSpotRetry(ctx context.Context, symbol string, since time.Time, required bool) ([]Bar, error) {
	attempts := 2
	if required {
		attempts = 5
	}
	var lastErr error
	for a := 0; a < attempts; a++ {
		if a > 0 {
			delay := time.Duration(250*(1<<uint(a))) * time.Millisecond // 0.5s, 1s, 2s, 4s
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}
		bars, err := bitgetSpot(ctx, symbol, since)
		if err == nil && len(bars) > 0 {
			return bars, nil
		}
		if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("%s: no candles returned", symbol)
		}
	}
	return nil, lastErr
}

func probeEquityHistorical(ctx context.Context, client *mcp.Client) string {
	end := time.Now().UTC()
	start := end.AddDate(0, 0, -120)
	res, err := client.Query(ctx, "equity_price_historical", map[string]any{
		"symbol":     "SPY",
		"start_time": start.UnixMilli(),
		"end_time":   end.UnixMilli(),
	})
	if err != nil {
		return "equity/price/historical (SPY) was not used: " + err.Error()
	}
	if res.StatusCode == 204 || len(res.Data) == 0 || string(res.Data) == `""` {
		return "equity/price/historical for SPY returned empty (HTTP 204). The backtest prices the equity sleeve on Bitget Reality rSPYUSDT instead of mixing in another source."
	}
	var wrap struct {
		Results []json.RawMessage `json:"results"`
	}
	_ = json.Unmarshal(res.Data, &wrap)
	return fmt.Sprintf("equity/price/historical for SPY returned %d rows (HTTP %d). Those rows are not mixed into the backtest; the equity sleeve is rSPYUSDT.", len(wrap.Results), res.StatusCode)
}

func align(equity, btc, gold []Bar, sectors map[string][]Bar) []Day {
	btcBy := byDate(btc)
	goldBy := byDate(gold)
	sectorBy := make(map[string]map[int64]Bar, len(sectors))
	for key, bars := range sectors {
		sectorBy[key] = byDate(bars)
	}
	eq := weekdayBars(clean(equity, time.Time{}))
	var days []Day
	var lastBTC, lastGold Bar
	var haveBTC, haveGold bool
	lastSector := map[string]Bar{}
	for _, bar := range eq {
		if b, ok := btcBy[bar.Date.Unix()]; ok {
			lastBTC = b
			haveBTC = true
		} else if haveBTC && bar.Date.Sub(lastBTC.Date) > 4*24*time.Hour {
			haveBTC = false
		}
		if b, ok := goldBy[bar.Date.Unix()]; ok {
			lastGold = b
			haveGold = true
		} else if haveGold && bar.Date.Sub(lastGold.Date) > 4*24*time.Hour {
			haveGold = false
		}
		if !haveBTC || !haveGold || lastBTC.Close <= 0 || lastGold.Close <= 0 {
			continue
		}
		stocks := make(map[string]float64, len(sectorBy))
		for key, series := range sectorBy {
			if b, ok := series[bar.Date.Unix()]; ok {
				lastSector[key] = b
			} else if prev, ok := lastSector[key]; ok && bar.Date.Sub(prev.Date) > 4*24*time.Hour {
				delete(lastSector, key)
			}
			if prev, ok := lastSector[key]; ok && prev.Close > 0 {
				stocks[key] = prev.Close
			}
		}
		days = append(days, Day{
			Date:       bar.Date,
			Equity:     bar.Close,
			EquityOpen: bar.Open,
			BTC:        lastBTC.Close,
			Gold:       lastGold.Close,
			Stocks:     stocks,
		})
	}
	return days
}

func byDate(bars []Bar) map[int64]Bar {
	m := make(map[int64]Bar, len(bars))
	for _, b := range bars {
		b.Date = dateOnly(b.Date)
		if b.Close <= 0 || !b.Date.Before(dateOnly(time.Now().UTC())) {
			continue
		}
		m[b.Date.Unix()] = b
	}
	return m
}

func clean(bars []Bar, since time.Time) []Bar {
	today := dateOnly(time.Now().UTC())
	seen := map[int64]Bar{}
	for _, b := range bars {
		b.Date = dateOnly(b.Date)
		if b.Close <= 0 || !b.Date.Before(today) {
			continue
		}
		if !since.IsZero() && b.Date.Before(dateOnly(since)) {
			continue
		}
		seen[b.Date.Unix()] = b
	}
	out := make([]Bar, 0, len(seen))
	for _, b := range seen {
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date.Before(out[j].Date) })
	return out
}

func weekdayBars(bars []Bar) []Bar {
	out := make([]Bar, 0, len(bars))
	for _, b := range bars {
		wd := b.Date.Weekday()
		if wd == time.Saturday || wd == time.Sunday {
			continue
		}
		out = append(out, b)
	}
	return out
}

func dateOnly(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
}

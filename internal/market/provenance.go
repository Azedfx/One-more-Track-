package market

import (
	"fmt"
	"time"
)

// RTokenLaunch is the day Bitget Reality listed its rToken US-stock pairs
// (rSPYUSDT and the sector tokens) on the spot market, per Bitget's own
// announcement of Stocks 2.0 / Reality. Candles the exchange serves before
// this date are the underlying reference price (the SPY-equivalent NAV), not
// live rToken market prints. On and after this date the same symbol carries
// genuine 24/7 rToken trades.
//
// Source: Bitget, "Reality" / "Stocks 2.0" launch, 2026-06-02
// (https://www.bitget.com/campaigns/bitget-rtoken).
var RTokenLaunch = time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)

// Provenance summarises how much of the aligned panel is genuine live rToken
// data versus Bitget's pre-listing reference backfill. It is attached to the
// Book so the page, the CSV note, and the -once dump can all state it plainly
// instead of implying the whole history is on-chain rToken trading.
type Provenance struct {
	Launch      time.Time
	FirstDay    time.Time
	LastDay     time.Time
	TotalDays   int
	LiveDays    int // aligned weekdays on or after the launch
	BackfillEnd time.Time
	Note        string
}

func provenanceOf(days []Day) Provenance {
	p := Provenance{Launch: RTokenLaunch}
	if len(days) == 0 {
		return p
	}
	p.FirstDay = days[0].Date
	p.LastDay = days[len(days)-1].Date
	p.TotalDays = len(days)
	for _, d := range days {
		if !d.Date.Before(RTokenLaunch) {
			p.LiveDays++
		} else {
			p.BackfillEnd = d.Date
		}
	}
	p.Note = fmt.Sprintf(
		"The rToken pairs (rSPYUSDT and the sector tokens) listed on Bitget on %s. "+
			"Candles before that day are Bitget's underlying reference price for the same stock/ETF, not live rToken trades; "+
			"candles on and after it are genuine rToken market prints. "+
			"Of %d aligned weekdays, %d fall in the live-rToken window. "+
			"BTCUSDT and PAXGUSDT are ordinary spot pairs and are live throughout.",
		RTokenLaunch.Format("2006-01-02"), p.TotalDays, p.LiveDays,
	)
	return p
}

// LiveAsOf reports whether a date sits in the genuine live-rToken window.
func LiveAsOf(t time.Time) bool {
	return !t.Before(RTokenLaunch)
}

// labelProvenance appends the splice note to each rToken series so the source
// table on the page never implies the full history is live rToken trading.
// rToken symbols are the ones prefixed with "r" (rSPYUSDT, rXLKUSDT, ...);
// BTCUSDT and PAXGUSDT are ordinary spot pairs and are left as-is.
func labelProvenance(meta []SeriesMeta) []SeriesMeta {
	for i := range meta {
		if len(meta[i].Symbol) == 0 || meta[i].Symbol[0] != 'r' {
			continue
		}
		tag := fmt.Sprintf("Live rToken from %s; earlier bars are Bitget's underlying reference price.", RTokenLaunch.Format("2006-01-02"))
		if meta[i].Note == "" {
			meta[i].Note = tag
		} else {
			meta[i].Note = meta[i].Note + " " + tag
		}
	}
	return meta
}

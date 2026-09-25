package backtest

import (
	"math"
	"sort"
	"time"

	"regime-sleeve/internal/strategy"
)

func summarize(points []Point, ppy float64, withTrades bool) Metrics {
	m := fromReturns(returnsOf(points, func(p Point) float64 { return p.Ret }), ppy)
	if len(points) > 0 {
		m.Start = points[0].Date
		m.End = points[len(points)-1].Date
		m.CAGR = cagr(1+m.TotalReturn, m.Start, m.End)
	}
	for _, p := range points {
		if p.Switched {
			m.Switches++
		}
	}
	m.Turnover = annualTurnover(m.Switches, m.Start, m.End)
	if withTrades {
		m.Trades, m.TradeWin = tradeStats(points)
	}
	return m
}

func summarizeHold(points []Point, ppy float64) Metrics {
	m := fromReturns(returnsOf(points, func(p Point) float64 { return p.HoldRet }), ppy)
	if len(points) > 0 {
		m.Start = points[0].Date
		m.End = points[len(points)-1].Date
		m.CAGR = cagr(1+m.TotalReturn, m.Start, m.End)
	}
	return m
}

func summarizeBlend(points []Point, ppy float64) Metrics {
	m := fromReturns(returnsOf(points, func(p Point) float64 { return p.BlendRet }), ppy)
	if len(points) > 0 {
		m.Start = points[0].Date
		m.End = points[len(points)-1].Date
		m.CAGR = cagr(1+m.TotalReturn, m.Start, m.End)
	}
	return m
}

func returnsOf(points []Point, pick func(Point) float64) []float64 {
	out := make([]float64, len(points))
	for i, p := range points {
		out[i] = pick(p)
	}
	return out
}

func fromReturns(rets []float64, ppy float64) Metrics {
	m := Metrics{Days: len(rets)}
	if len(rets) == 0 {
		return m
	}
	eq := 1.0
	peak := 1.0
	maxDD := 0.0
	wins := 0
	gain, loss := 0.0, 0.0
	for _, r := range rets {
		eq *= 1 + r
		if eq > peak {
			peak = eq
		}
		dd := eq/peak - 1
		if dd < maxDD {
			maxDD = dd
		}
		switch {
		case r > 0:
			wins++
			gain += r
		case r < 0:
			loss += -r
		}
	}
	m.TotalReturn = eq - 1
	m.MaxDrawdown = maxDD
	m.WinRate = float64(wins) / float64(len(rets))
	if loss > 0 {
		m.ProfitFactor = gain / loss
		m.HasPF = true
	}
	mean, std := meanStd(rets)
	if std > 0 && ppy > 0 {
		m.Sharpe = mean / std * math.Sqrt(ppy)
	}
	if down := downside(rets); down > 0 && ppy > 0 {
		m.Sortino = mean / down * math.Sqrt(ppy)
	}
	return m
}

func cagr(finalEquity float64, start, end time.Time) float64 {
	span := end.Sub(start).Hours() / 24
	if span <= 1 || finalEquity <= 0 {
		return 0
	}
	return math.Pow(finalEquity, 365.25/span) - 1
}

func annualTurnover(switches int, start, end time.Time) float64 {
	years := end.Sub(start).Hours() / 24 / 365.25
	if years <= 0 {
		return 0
	}
	return float64(switches) / years
}

func tradeStats(points []Point) (int, float64) {
	trades := 0
	wins := 0
	var cur strategy.Sleeve
	eq := 1.0
	open := false
	flush := func() {
		if !open {
			return
		}
		trades++
		if eq > 1 {
			wins++
		}
		eq = 1
		open = false
	}
	for _, p := range points {
		if p.Sleeve == strategy.Cash {
			flush()
			cur = ""
			continue
		}
		if p.Sleeve != cur {
			flush()
			cur = p.Sleeve
			open = true
			eq = 1 + p.Ret
			continue
		}
		eq *= 1 + p.Ret
	}
	flush()
	if trades == 0 {
		return 0, 0
	}
	return trades, float64(wins) / float64(trades)
}

func rolling(rets []float64, window int, ppy float64) RollStats {
	stats := RollStats{Window: window}
	if len(rets) < window || window < 2 {
		return stats
	}
	vals := make([]float64, 0, len(rets)-window+1)
	for i := window; i <= len(rets); i++ {
		mean, std := meanStd(rets[i-window : i])
		sharpe := 0.0
		if std > 0 && ppy > 0 {
			sharpe = mean / std * math.Sqrt(ppy)
		}
		vals = append(vals, sharpe)
	}
	sorted := append([]float64(nil), vals...)
	sort.Float64s(sorted)
	pos := 0
	for _, v := range vals {
		if v > 0 {
			pos++
		}
	}
	stats.Count = len(vals)
	stats.Min = sorted[0]
	stats.Median = sorted[len(sorted)/2]
	stats.Positive = float64(pos) / float64(len(vals))
	stats.Values = vals
	return stats
}

func meanStd(xs []float64) (float64, float64) {
	if len(xs) == 0 {
		return 0, 0
	}
	sum := 0.0
	for _, x := range xs {
		sum += x
	}
	mean := sum / float64(len(xs))
	if len(xs) < 2 {
		return mean, 0
	}
	ss := 0.0
	for _, x := range xs {
		d := x - mean
		ss += d * d
	}
	return mean, math.Sqrt(ss / float64(len(xs)-1))
}

func sampleStd(xs []float64) float64 {
	_, sd := meanStd(xs)
	return sd
}

func downside(xs []float64) float64 {
	if len(xs) < 2 {
		return 0
	}
	ss := 0.0
	for _, x := range xs {
		if x < 0 {
			ss += x * x
		}
	}
	return math.Sqrt(ss / float64(len(xs)-1))
}

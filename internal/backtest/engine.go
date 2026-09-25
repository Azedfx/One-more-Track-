package backtest

import (
	"fmt"
	"math"
	"time"

	"regime-sleeve/internal/market"
	"regime-sleeve/internal/strategy"
)

// Point is one weekday the strategy actually traded.
type Point struct {
	Date     time.Time
	Sleeve   strategy.Sleeve
	Ret      float64
	HoldRet  float64
	BlendRet float64
	Gap      float64
	Intra    float64
	InEquity bool
	Switched bool
}

// Snapshot is the rule output at the last completed close.
type Snapshot struct {
	AsOf        time.Time
	LastSleeve  strategy.Sleeve
	NextSleeve  strategy.Sleeve
	Equity      float64
	EquitySMA   float64
	EquityUp    bool
	RealizedVol float64
	VolCalm     bool
	BTC         float64
	BTCSMA      float64
	BTCUp       bool
	Gold        float64
	GoldSMA     float64
	GoldUp      bool
}

// Result is the full run, including the 40 bp cost sensitivity.
type Result struct {
	Spec      strategy.Spec
	PPY       float64
	Points    []Point
	Full      Trio
	InSample  Trio
	OutSample Trio
	Harsh     Metrics
	HarshCost float64 // one-way cost used in the Harsh sensitivity run
	Roll      RollStats
	Snap      Snapshot
	GapShare  float64
	GapDays   int
	HasGap    bool
	OOSDays   int
	Alert     bool
	AlertText string
}

// Trio holds the strategy and the two benchmarks on the same dates.
type Trio struct {
	Strategy Metrics
	Hold     Metrics
	Blend    Metrics
}

// Metrics are the figures the Alpha Factory rubric asks for.
type Metrics struct {
	Days         int
	Start        time.Time
	End          time.Time
	TotalReturn  float64
	CAGR         float64
	Sharpe       float64
	Sortino      float64
	MaxDrawdown  float64
	WinRate      float64
	TradeWin     float64
	Trades       int
	ProfitFactor float64
	Turnover     float64
	Switches     int
	HasPF        bool
}

// RollStats summarises the 30-day rolling Sharpe.
type RollStats struct {
	Window   int
	Count    int
	Min      float64
	Median   float64
	Positive float64
	Values   []float64
}

// Run applies the frozen sleeve rule. Signals at the prior close earn the
// next close-to-close return. Weekend bars are already removed by the loader.
func Run(days []market.Day, spec strategy.Spec) (Result, error) {
	primary, err := runPath(days, spec)
	if err != nil {
		return Result{}, err
	}
	harshSpec := spec.HarshCost()
	harsh, err := runPath(days, harshSpec)
	if err != nil {
		return Result{}, err
	}
	primary.Harsh = harsh.Full.Strategy
	primary.HarshCost = harshSpec.CostRate
	return primary, nil
}

func runPath(days []market.Day, spec strategy.Spec) (Result, error) {
	need := spec.EquitySMA
	if spec.AltSMA > need {
		need = spec.AltSMA
	}
	if spec.VolWindow > need {
		need = spec.VolWindow
	}
	if len(days) <= need {
		return Result{}, fmt.Errorf("need more than %d weekday bars, have %d", need, len(days))
	}
	start := need
	tradable := len(days) - start
	oos := spec.OOSDays
	if tradable < oos+60 {
		oos = 30
	}
	if tradable < oos+60 {
		return Result{}, fmt.Errorf("only %d tradable weekdays after warmup; need at least %d", tradable, oos+60)
	}

	ppy := periodsPerYear(days)
	eq := column(days, func(d market.Day) float64 { return d.Equity })
	btc := column(days, func(d market.Day) float64 { return d.BTC })
	gold := column(days, func(d market.Day) float64 { return d.Gold })
	stocks := stockColumns(days)

	points := make([]Point, 0, tradable)
	prev := strategy.Cash
	held := 0 // weekday bars the current sleeve has been held
	w := [3]float64{}
	var gapSum, intraSum float64
	gapDays := 0

	for i := start; i < len(days); i++ {
		chosen := choose(eq, btc, gold, stocks, i-1, spec, ppy, prev)
		// Minimum holding period: once in a sleeve, ignore new signals until it
		// has been held long enough. This throttles whipsaw turnover.
		if spec.MinHold > 0 && prev != strategy.Cash && held < spec.MinHold && chosen != prev {
			chosen = prev
		}
		switched := chosen != prev
		cost := 0.0
		if switched {
			cost = spec.CostRate
		}
		ret := sleeveReturn(chosen, eq, btc, gold, stocks, i) - cost

		holdCost := 0.0
		if i == start {
			holdCost = spec.CostRate
		}
		holdRet := simple(eq, i) - holdCost

		blendRet, nextW := blendStep(w, i == start || newMonth(days[i-1].Date, days[i].Date), eq, btc, gold, i, spec.CostRate)
		w = nextW

		var gap, intra float64
		inEquity := chosen == strategy.Equity
		if inEquity && days[i].EquityOpen > 0 && eq[i-1] > 0 {
			gap = days[i].EquityOpen/eq[i-1] - 1
			intra = eq[i]/days[i].EquityOpen - 1
			gapSum += gap
			intraSum += intra
			gapDays++
		}

		points = append(points, Point{
			Date:     days[i].Date,
			Sleeve:   chosen,
			Ret:      ret,
			HoldRet:  holdRet,
			BlendRet: blendRet,
			Gap:      gap,
			Intra:    intra,
			InEquity: inEquity,
			Switched: switched,
		})
		if switched {
			held = 1
		} else {
			held++
		}
		prev = chosen
	}

	isEnd := len(points) - oos
	full := Trio{
		Strategy: summarize(points, ppy, true),
		Hold:     summarizeHold(points, ppy),
		Blend:    summarizeBlend(points, ppy),
	}
	inS := Trio{Strategy: summarize(points[:isEnd], ppy, true)}
	outS := Trio{
		Strategy: summarize(points[isEnd:], ppy, true),
		Hold:     summarizeHold(points[isEnd:], ppy),
		Blend:    summarizeBlend(points[isEnd:], ppy),
	}

	alert := false
	alertText := "In-sample Sharpe is not positive, so the handbook's 0.5× out-of-sample decay check does not apply."
	if inS.Strategy.Sharpe > 0 {
		if outS.Strategy.Sharpe < 0.5*inS.Strategy.Sharpe {
			alert = true
			alertText = fmt.Sprintf("Out-of-sample Sharpe %.2f is below 0.5× the in-sample Sharpe %.2f. The handbook flags this as decay.", outS.Strategy.Sharpe, inS.Strategy.Sharpe)
		} else {
			alertText = fmt.Sprintf("Out-of-sample Sharpe %.2f is at least half the in-sample Sharpe %.2f.", outS.Strategy.Sharpe, inS.Strategy.Sharpe)
		}
	}

	res := Result{
		Spec:      spec,
		PPY:       ppy,
		Points:    points,
		Full:      full,
		InSample:  inS,
		OutSample: outS,
		Roll:      rolling(strategyRets(points), 30, ppy),
		Snap:      snapshot(days, eq, btc, gold, stocks, points, spec, ppy),
		OOSDays:   oos,
		Alert:     alert,
		AlertText: alertText,
		GapDays:   gapDays,
	}
	if gapDays > 0 && math.Abs(gapSum+intraSum) > 1e-9 {
		res.GapShare = gapSum / (gapSum + intraSum)
		res.HasGap = true
	}
	return res, nil
}

func choose(eq, btc, gold []float64, stocks map[string][]float64, end int, spec strategy.Spec, ppy float64, prev strategy.Sleeve) strategy.Sleeve {
	eqSMA := sma(eq, end, spec.EquitySMA)
	vol := realizedVol(eq, end, spec.VolWindow, ppy)
	btcSMA := sma(btc, end, spec.AltSMA)
	goldSMA := sma(gold, end, spec.AltSMA)
	// A symmetric band around each average makes the signal sticky: a sleeve
	// currently held only has to stay above avg*(1-band), while a new sleeve has
	// to clear avg*(1+band). This removes churn from prices hugging the line.
	equityUp := above(eq[end], eqSMA, spec.Band, strategy.IsStock(prev))
	calm := vol < spec.VolStress
	switch {
	case equityUp && calm:
		if spec.HoldBroad {
			return strategy.Equity
		}
		return pickSector(eq, stocks, end, spec)
	case !equityUp && calm && above(btc[end], btcSMA, spec.Band, prev == strategy.Crypto):
		return strategy.Crypto
	case above(gold[end], goldSMA, spec.Band, prev == strategy.Gold):
		return strategy.Gold
	default:
		return strategy.Cash
	}
}

// above reports whether price clears its average, with a hysteresis band. When
// the sleeve is already held, the bar is lowered to avg*(1-band); otherwise it
// is raised to avg*(1+band).
func above(price, avg, band float64, held bool) bool {
	if math.IsNaN(avg) {
		return false
	}
	if band <= 0 {
		return price > avg
	}
	if held {
		return price > avg*(1-band)
	}
	return price > avg*(1+band)
}

// pickSector holds the sector token with the strongest trailing return,
// provided it is also above its own average. The broad market is the fallback.
func pickSector(eq []float64, stocks map[string][]float64, end int, spec strategy.Spec) strategy.Sleeve {
	look := spec.LeaderLookback
	window := spec.LeaderSMA
	if look <= 0 {
		look = 60
	}
	if window <= 0 {
		window = 50
	}
	best := strategy.Equity
	bestRet := math.Inf(-1)
	consider := func(name strategy.Sleeve, xs []float64) {
		ret, ok := trailingUp(xs, end, look, window)
		if ok && ret > bestRet {
			bestRet = ret
			best = name
		}
	}
	consider(strategy.Equity, eq)
	for _, item := range strategy.Listings() {
		if item.Role != "sector" {
			continue
		}
		consider(strategy.Sleeve(item.Key), stocks[item.Key])
	}
	return best
}

func trailingUp(xs []float64, end, look, window int) (float64, bool) {
	if xs == nil || end < look || end < window-1 || end >= len(xs) {
		return 0, false
	}
	avg, ok := smaStrict(xs, end, window)
	if !ok || xs[end] <= avg || xs[end-look] <= 0 || xs[end] <= 0 {
		return 0, false
	}
	return xs[end]/xs[end-look] - 1, true
}

func smaStrict(xs []float64, end, window int) (float64, bool) {
	if end < window-1 || end >= len(xs) {
		return 0, false
	}
	sum := 0.0
	for i := end - window + 1; i <= end; i++ {
		if xs[i] <= 0 {
			return 0, false
		}
		sum += xs[i]
	}
	return sum / float64(window), true
}

func snapshot(days []market.Day, eq, btc, gold []float64, stocks map[string][]float64, points []Point, spec strategy.Spec, ppy float64) Snapshot {
	end := len(days) - 1
	last := strategy.Cash
	if len(points) > 0 {
		last = points[len(points)-1].Sleeve
	}
	return Snapshot{
		AsOf:        days[end].Date,
		LastSleeve:  last,
		NextSleeve:  choose(eq, btc, gold, stocks, end, spec, ppy, last),
		Equity:      eq[end],
		EquitySMA:   sma(eq, end, spec.EquitySMA),
		EquityUp:    eq[end] > sma(eq, end, spec.EquitySMA),
		RealizedVol: realizedVol(eq, end, spec.VolWindow, ppy),
		VolCalm:     realizedVol(eq, end, spec.VolWindow, ppy) < spec.VolStress,
		BTC:         btc[end],
		BTCSMA:      sma(btc, end, spec.AltSMA),
		BTCUp:       btc[end] > sma(btc, end, spec.AltSMA),
		Gold:        gold[end],
		GoldSMA:     sma(gold, end, spec.AltSMA),
		GoldUp:      gold[end] > sma(gold, end, spec.AltSMA),
	}
}

func sleeveReturn(s strategy.Sleeve, eq, btc, gold []float64, stocks map[string][]float64, i int) float64 {
	switch s {
	case strategy.Equity:
		return simple(eq, i)
	case strategy.Crypto:
		return simple(btc, i)
	case strategy.Gold:
		return simple(gold, i)
	case strategy.Cash, "":
		return 0
	default:
		xs := stocks[string(s)]
		if xs == nil {
			return 0
		}
		return simple(xs, i)
	}
}

func simple(xs []float64, i int) float64 {
	if i <= 0 || xs[i-1] <= 0 || xs[i] <= 0 {
		return 0
	}
	return xs[i]/xs[i-1] - 1
}

func blendStep(w [3]float64, rebalance bool, eq, btc, gold []float64, i int, costRate float64) (float64, [3]float64) {
	cost := 0.0
	if rebalance {
		target := [3]float64{1.0 / 3, 1.0 / 3, 1.0 / 3}
		buy := 0.0
		for k := 0; k < 3; k++ {
			if target[k] > w[k] {
				buy += target[k] - w[k]
			}
		}
		cost = buy * costRate
		w = target
	}
	r := [3]float64{simple(eq, i), simple(btc, i), simple(gold, i)}
	ret := w[0]*r[0] + w[1]*r[1] + w[2]*r[2] - cost
	gross := [3]float64{w[0] * (1 + r[0]), w[1] * (1 + r[1]), w[2] * (1 + r[2])}
	sum := gross[0] + gross[1] + gross[2]
	if sum > 0 {
		w = [3]float64{gross[0] / sum, gross[1] / sum, gross[2] / sum}
	}
	return ret, w
}

func newMonth(prev, cur time.Time) bool {
	return prev.Year() != cur.Year() || prev.Month() != cur.Month()
}

func sma(xs []float64, end, window int) float64 {
	if end < window-1 || end >= len(xs) {
		return math.NaN()
	}
	sum := 0.0
	for i := end - window + 1; i <= end; i++ {
		sum += xs[i]
	}
	return sum / float64(window)
}

func realizedVol(xs []float64, end, window int, ppy float64) float64 {
	if end < window || end >= len(xs) {
		return math.NaN()
	}
	rets := make([]float64, 0, window)
	for i := end - window + 1; i <= end; i++ {
		if xs[i-1] <= 0 || xs[i] <= 0 {
			return math.NaN()
		}
		rets = append(rets, xs[i]/xs[i-1]-1)
	}
	sd := sampleStd(rets)
	if ppy <= 0 {
		ppy = 252
	}
	return sd * math.Sqrt(ppy)
}

func periodsPerYear(days []market.Day) float64 {
	if len(days) < 2 {
		return 252
	}
	span := days[len(days)-1].Date.Sub(days[0].Date).Hours() / 24
	if span < 1 {
		return 252
	}
	return float64(len(days)-1) / (span / 365.25)
}

func column(days []market.Day, pick func(market.Day) float64) []float64 {
	out := make([]float64, len(days))
	for i, d := range days {
		out[i] = pick(d)
	}
	return out
}

func stockColumns(days []market.Day) map[string][]float64 {
	keys := map[string]struct{}{}
	for _, d := range days {
		for key := range d.Stocks {
			keys[key] = struct{}{}
		}
	}
	out := make(map[string][]float64, len(keys))
	for key := range keys {
		xs := make([]float64, len(days))
		for i, d := range days {
			if d.Stocks != nil {
				xs[i] = d.Stocks[key]
			}
		}
		out[key] = xs
	}
	return out
}

func strategyRets(points []Point) []float64 {
	out := make([]float64, len(points))
	for i, p := range points {
		out[i] = p.Ret
	}
	return out
}

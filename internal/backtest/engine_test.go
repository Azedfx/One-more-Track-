package backtest

import (
	"math"
	"testing"
	"time"

	"regime-sleeve/internal/market"
	"regime-sleeve/internal/strategy"
)

func TestUptrendStaysInEquityAndChargesEntryOnce(t *testing.T) {
	days := ramp(260, 1.0005, 1, 1)
	// Test the entry mechanic with no hysteresis band so the switch lands on the
	// first bar past warmup, as the return assertions below expect.
	spec := strategy.Default()
	spec.Band = 0
	res, err := Run(days, spec)
	if err != nil {
		t.Fatal(err)
	}
	if res.Points[0].Sleeve != strategy.Equity {
		t.Fatalf("first sleeve %s", res.Points[0].Sleeve)
	}
	switches := 0
	for _, p := range res.Points {
		if p.Sleeve != strategy.Equity {
			t.Fatalf("left equity for %s on %s", p.Sleeve, p.Date)
		}
		if p.Switched {
			switches++
		}
	}
	if switches != 1 {
		t.Fatalf("switches = %d, want 1", switches)
	}
	raw := days[100].Equity/days[99].Equity - 1
	if math.Abs(res.Points[0].Ret-(raw-0.0015)) > 1e-9 {
		t.Fatalf("entry return %.6f, raw %.6f", res.Points[0].Ret, raw)
	}
	raw2 := days[101].Equity/days[100].Equity - 1
	if math.Abs(res.Points[1].Ret-raw2) > 1e-9 {
		t.Fatalf("second day charged a cost: got %.6f want %.6f", res.Points[1].Ret, raw2)
	}
	if res.Full.Strategy.Sharpe <= 0 {
		t.Fatalf("sharpe %.3f", res.Full.Strategy.Sharpe)
	}
	if res.OOSDays != 63 {
		t.Fatalf("oos days %d", res.OOSDays)
	}
	if res.InSample.Strategy.Days+res.OutSample.Strategy.Days != len(res.Points) {
		t.Fatal("is/oos split does not cover the path")
	}
}

func TestLastCloseDoesNotChangePriorSleeve(t *testing.T) {
	days := ramp(260, 1.0004, 1, 1.0002)
	// No band so the gentle synthetic ramp still enters equity; this test is
	// about lookahead, not hysteresis.
	spec := strategy.Default()
	spec.Band = 0
	base, err := Run(days, spec)
	if err != nil {
		t.Fatal(err)
	}
	shocked := append([]market.Day(nil), days...)
	last := len(shocked) - 1
	shocked[last].Equity = shocked[last].Equity * 3
	next, err := Run(shocked, spec)
	if err != nil {
		t.Fatal(err)
	}
	i := len(base.Points) - 2
	if base.Points[i].Sleeve != next.Points[i].Sleeve || math.Abs(base.Points[i].Ret-next.Points[i].Ret) > 1e-12 {
		t.Fatal("a last-bar shock leaked into the previous day")
	}
	j := len(base.Points) - 1
	if base.Points[j].Sleeve != next.Points[j].Sleeve {
		t.Fatal("last sleeve used its own close")
	}
	if math.Abs(base.Points[j].Ret-next.Points[j].Ret) < 1e-12 {
		t.Fatal("last return ignored its own close")
	}
}

func TestStressRotatesIntoGold(t *testing.T) {
	days := ramp(180, 1.001, 1, 1.002)
	for i := 180; i < 260; i++ {
		prev := days[i-1]
		days = append(days, market.Day{
			Date:       prev.Date.AddDate(0, 0, 1),
			Equity:     prev.Equity * 0.97,
			EquityOpen: prev.Equity * 0.985,
			BTC:        prev.BTC,
			Gold:       prev.Gold * 1.002,
		})
	}
	res, err := Run(days, strategy.Default())
	if err != nil {
		t.Fatal(err)
	}
	// Well after the crash starts, the equity trend is broken and vol is hot.
	if res.Points[len(res.Points)-1].Sleeve != strategy.Gold {
		t.Fatalf("last sleeve %s, want PAXG", res.Points[len(res.Points)-1].Sleeve)
	}
}

func TestLeaderPicksTheStrongerSector(t *testing.T) {
	days := ramp(260, 1.0005, 1, 1)
	px := 100.0
	for i := range days {
		if i > 0 {
			px *= 1.002
		}
		days[i].Stocks = map[string]float64{"rXLK": px}
	}
	// The leader overlay is off in the frozen default; exercise it explicitly.
	res, err := Run(days, strategy.Default().SectorChase())
	if err != nil {
		t.Fatal(err)
	}
	if res.Points[len(res.Points)-1].Sleeve != "rXLK" {
		t.Fatalf("last sleeve %s, want rXLK", res.Points[len(res.Points)-1].Sleeve)
	}
}

func ramp(n int, eqStep, btcStep, goldStep float64) []market.Day {
	start := time.Date(2022, 1, 3, 0, 0, 0, 0, time.UTC)
	days := make([]market.Day, n)
	eq, btc, gold := 100.0, 100.0, 100.0
	for i := 0; i < n; i++ {
		if i > 0 {
			eq *= eqStep
			btc *= btcStep
			gold *= goldStep
		}
		days[i] = market.Day{
			Date:       start.AddDate(0, 0, i),
			Equity:     eq,
			EquityOpen: eq / eqStep,
			BTC:        btc,
			Gold:       gold,
		}
	}
	days[0].EquityOpen = 100
	return days
}

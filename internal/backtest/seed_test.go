package backtest

import (
	"testing"

	"regime-sleeve/internal/market"
	"regime-sleeve/internal/strategy"
)

// TestSeedDatasetRuns is the deploy safety net: the bundled seed must decode
// and produce a valid backtest, since it is the last-resort data source when a
// host cannot reach Bitget.
func TestSeedDatasetRuns(t *testing.T) {
	book, ok := market.Seed()
	if !ok {
		t.Fatal("embedded seed failed to decode")
	}
	if len(book.Days) < 160 {
		t.Fatalf("seed has only %d days, need at least 160", len(book.Days))
	}
	res, err := Run(book.Days, strategy.Default())
	if err != nil {
		t.Fatalf("backtest on seed failed: %v", err)
	}
	if res.Full.Strategy.Sharpe <= 0 {
		t.Fatalf("seed backtest sharpe %.2f, want > 0", res.Full.Strategy.Sharpe)
	}
	if res.OutSample.Strategy.Days < 30 {
		t.Fatalf("seed out-of-sample only %d days, need >= 30", res.OutSample.Strategy.Days)
	}
}

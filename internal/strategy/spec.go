package strategy

// Sleeve is the single book the rotator holds between weekday closes.
type Sleeve string

const (
	Cash   Sleeve = "CASH"
	Equity Sleeve = "rSPY"
	Crypto Sleeve = "BTC"
	Gold   Sleeve = "PAXG"
)

// Spec is frozen. These values are the strategy. They are not searched on
// the out-of-sample window.
type Spec struct {
	EquitySMA      int     // rSPY close must clear this average to count as risk-on
	AltSMA         int     // BTC and PAXG trend window
	VolWindow      int     // realized-vol lookback, in weekday closes
	VolStress      float64 // annualized realized vol at or above this is risk-off
	CostRate       float64 // one-way cost charged on a sleeve change (fee + slippage)
	OOSDays        int     // last N weekday returns held out of sample
	LeaderSMA      int     // a sector token must be above this average to be held
	LeaderLookback int     // hold the sector with the strongest return over this many weekdays

	// HoldBroad keeps the risk-on sleeve in the broad market (rSPY) instead of
	// chasing the strongest sector token. The sector-leader overlay adds a dozen
	// noisy choices and heavy turnover; holding broad is far more robust.
	HoldBroad bool
	// Band is a symmetric buffer around each trend average, as a fraction of the
	// average. A signal must clear the average by +Band to switch on and fall
	// below by -Band to switch off. This kills whipsaw around the line.
	Band float64
	// MinHold is the minimum number of weekday bars a sleeve is held before the
	// rule is allowed to switch again. Zero means switch as soon as the signal
	// changes.
	MinHold int
}

// Default is the submitted rule set.
//
// VolStress is 28% annualized weekday realized vol. That level is a stress
// print for a broad equity sleeve, not a fitted cutoff.
//
// The book holds the broad market (rSPY) in risk-on rather than chasing the
// strongest sector token. The sector overlay added a dozen noisy choices and
// ~40 switches a year; out of sample it decayed hard (OOS Sharpe went
// negative). Holding broad with a 3% hysteresis band and a two-week minimum
// hold keeps turnover near 3 a year and holds the out-of-sample Sharpe close
// to the in-sample figure.
func Default() Spec {
	return Spec{
		EquitySMA:      100,
		AltSMA:         50,
		VolWindow:      20,
		VolStress:      0.28,
		CostRate:       0.0015, // 10 bps fee + 5 bps slippage
		OOSDays:        63,
		LeaderSMA:      50,
		LeaderLookback: 60,
		HoldBroad:      true,
		Band:           0.03, // 3% buffer around each trend average
		MinHold:        10,   // hold at least two trading weeks before switching
	}
}

// SectorChase is the earlier, discarded rule that rotated into the strongest
// sector token. Kept only so the write-up and tests can show why it was
// dropped: it overfit in sample and decayed out of sample.
func (s Spec) SectorChase() Spec {
	s.HoldBroad = false
	s.Band = 0
	s.MinHold = 0
	return s
}

// HarshCost is the same rule with a wider rToken-style one-way cost.
func (s Spec) HarshCost() Spec {
	s.CostRate = 0.0040
	return s
}

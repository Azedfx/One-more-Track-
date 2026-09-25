package strategy

// Listing is one Bitget instrument the book is allowed to hold.
// The set is frozen. It is the S&P sector tokens plus the two other
// asset classes named in the Cross-Asset Allocation brief.
type Listing struct {
	Key   string // sleeve id stored on each backtest day
	Pair  string // Bitget spot symbol
	Title string
	Role  string // market, sector, crypto, commodity
}

// Listings is the tradable set, in display order.
func Listings() []Listing {
	return []Listing{
		{"rSPY", "rSPYUSDT", "US market", "market"},
		{"rXLK", "rXLKUSDT", "Technology", "sector"},
		{"rXLF", "rXLFUSDT", "Financials", "sector"},
		{"rXLV", "rXLVUSDT", "Health care", "sector"},
		{"rXLE", "rXLEUSDT", "Energy", "sector"},
		{"rXLY", "rXLYUSDT", "Discretionary", "sector"},
		{"rXLP", "rXLPUSDT", "Staples", "sector"},
		{"rXLI", "rXLIUSDT", "Industrials", "sector"},
		{"rXLB", "rXLBUSDT", "Materials", "sector"},
		{"rXLU", "rXLUUSDT", "Utilities", "sector"},
		{"rXLRE", "rXLREUSDT", "Real estate", "sector"},
		{"rXLC", "rXLCUSDT", "Communications", "sector"},
		{"BTC", "BTCUSDT", "Bitcoin", "crypto"},
		{"PAXG", "PAXGUSDT", "Gold", "commodity"},
	}
}

// Title is the plain name for a sleeve, including cash.
func Title(s Sleeve) string {
	if s == Cash || s == "" {
		return "Cash"
	}
	for _, item := range Listings() {
		if Sleeve(item.Key) == s {
			return item.Title
		}
	}
	return string(s)
}

// IsStock reports a US market or sector sleeve.
func IsStock(s Sleeve) bool {
	return s != "" && s != Cash && s != Crypto && s != Gold
}

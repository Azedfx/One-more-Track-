package market

import "time"

// Bar is one daily candle. Date is the UTC calendar day of the print.
type Bar struct {
	Date   time.Time
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume float64
}

// Day is one weekday on the rSPY calendar, with BTC and PAXG aligned to it.
type Day struct {
	Date       time.Time
	Equity     float64
	EquityOpen float64
	BTC        float64
	Gold       float64
	// Stocks holds sector-token closes keyed by sleeve id (rXLK, rXLF, ...).
	// Missing keys are names that had not listed yet.
	Stocks map[string]float64 `json:"stocks,omitempty"`
}

// SeriesMeta describes where a price series came from.
type SeriesMeta struct {
	Name   string
	Symbol string
	Source string
	Bars   int
	Start  time.Time
	End    time.Time
	Note   string
}

// Book is the aligned weekday panel the backtest runs on.
type Book struct {
	Days     []Day
	Meta     []SeriesMeta
	HistNote string
	Prov     Provenance
	Fetched  time.Time
}

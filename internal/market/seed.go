package market

import (
	_ "embed"
	"encoding/json"
	"time"
)

// seedMarket is a verified snapshot of the aligned price panel, compiled into
// the binary. It is the last-resort data source so the service always renders
// a backtest even when the host cannot reach Bitget (some cloud providers are
// geo-blocked by the public candle API). It is also the exact dataset behind
// the reported metrics, which makes the backtest reproducible.
//
//go:embed seed_market.json
var seedMarket []byte

// Seed decodes the embedded snapshot into a Book. The TTL is ignored; the
// snapshot is static by design.
func Seed() (Book, bool) {
	var file cacheFile
	if err := json.Unmarshal(seedMarket, &file); err != nil || len(file.Days) == 0 {
		return Book{}, false
	}
	prov := file.Prov
	if prov.TotalDays == 0 {
		prov = provenanceOf(file.Days)
	}
	fetched := file.Fetched
	if fetched.IsZero() {
		fetched = time.Now().UTC()
	}
	return Book{
		Days:     file.Days,
		Meta:     file.Meta,
		HistNote: file.HistNote,
		Prov:     prov,
		Fetched:  fetched,
	}, true
}

// LoadCachedStale reads a cached Book ignoring the freshness TTL. It is used as
// a fallback when a live refresh fails but an older good cache exists on disk.
func LoadCachedStale(path string) (Book, bool) {
	return loadCached(path, true)
}

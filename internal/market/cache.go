package market

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const cacheTTL = 6 * time.Hour

type cacheFile struct {
	Fetched  time.Time    `json:"fetched"`
	Days     []Day        `json:"days"`
	Meta     []SeriesMeta `json:"meta"`
	HistNote string       `json:"hist_note"`
	Prov     Provenance   `json:"provenance"`
}

func LoadCached(path string) (Book, bool) {
	return loadCached(path, false)
}

func loadCached(path string, ignoreTTL bool) (Book, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Book{}, false
	}
	var file cacheFile
	if err := json.Unmarshal(b, &file); err != nil {
		return Book{}, false
	}
	if len(file.Days) == 0 {
		return Book{}, false
	}
	if !ignoreTTL && time.Since(file.Fetched) > cacheTTL {
		return Book{}, false
	}
	prov := file.Prov
	if prov.TotalDays == 0 {
		prov = provenanceOf(file.Days) // backfill for caches written before provenance existed
	}
	return Book{
		Days:     file.Days,
		Meta:     file.Meta,
		HistNote: file.HistNote,
		Prov:     prov,
		Fetched:  file.Fetched,
	}, true
}

func SaveCache(path string, book Book) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	payload := cacheFile{
		Fetched:  book.Fetched,
		Days:     book.Days,
		Meta:     book.Meta,
		HistNote: book.HistNote,
		Prov:     book.Prov,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

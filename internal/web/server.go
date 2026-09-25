package web

import (
	"context"
	"embed"
	"encoding/csv"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"sync"
	"time"

	"regime-sleeve/internal/backtest"
	"regime-sleeve/internal/config"
	"regime-sleeve/internal/market"
	"regime-sleeve/internal/strategy"
)

//go:embed templates static
var assets embed.FS

const cachePath = ".cache/market-v2.json"

// Server renders the backtest. The page is complete without HTMX; HTMX only refreshes the results.
type Server struct {
	cfg  config.Config
	tmpl *template.Template

	mu        sync.Mutex
	book      market.Book
	result    backtest.Result
	narrative string
	ready     bool
}

func New(cfg config.Config) (*Server, error) {
	tmpl, err := template.New("layout.html").Funcs(templateFuncs()).ParseFS(assets, "templates/*.html")
	if err != nil {
		return nil, err
	}
	return &Server{cfg: cfg, tmpl: tmpl}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	static, err := fs.Sub(assets, "static")
	if err != nil {
		panic(err)
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(static))))
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /backtest.csv", s.csv)
	mux.HandleFunc("GET /note", s.note)
	mux.HandleFunc("GET /backtest", s.backtest)
	mux.HandleFunc("GET /", s.page)
	return mux
}

// Compute loads prices and runs the frozen rule. Used by the server and by -once.
func (s *Server) Compute(fresh bool) (market.Book, backtest.Result, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ready && !fresh {
		return s.book, s.result, s.narrative, nil
	}
	book, err := s.load(fresh)
	if err != nil {
		return market.Book{}, backtest.Result{}, "", err
	}
	result, err := backtest.Run(book.Days, strategy.Default())
	if err != nil {
		return market.Book{}, backtest.Result{}, "", err
	}
	narrative := plainExplain(result.Snap)
	s.book = book
	s.result = result
	s.narrative = narrative
	s.ready = true
	return book, result, narrative, nil
}

func (s *Server) load(fresh bool) (market.Book, error) {
	if !fresh {
		if book, ok := market.LoadCached(cachePath); ok {
			log.Printf("using prices saved at %s", book.Fetched.Format(time.RFC3339))
			return book, nil
		}
	}
	log.Printf("downloading daily prices from Bitget")
	book, err := market.Load(context.Background(), s.cfg.MCPURL)
	if err != nil {
		return market.Book{}, err
	}
	if err := market.SaveCache(cachePath, book); err != nil {
		log.Printf("cache write: %v", err)
	}
	return book, nil
}

func (s *Server) page(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	s.render(w, r, "layout")
}

func (s *Server) backtest(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "results")
}

func (s *Server) note(w http.ResponseWriter, r *http.Request) {
	_, result, _, err := s.Compute(false)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprint(w, explain(r.Context(), s.cfg, result.Snap))
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, name string) {
	fresh := r.URL.Query().Get("fresh") == "1"
	book, result, narrative, err := s.Compute(fresh)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	view := pageView{Narrative: narrative}
	if err != nil {
		view.Err = err.Error()
	} else {
		view.Book = book
		view.Result = result
		view.Mix = mixOf(result.Points)
		view.EquityChart = equitySVG(result.Points)
		view.DrawChart = drawdownSVG(result.Points)
		view.RollChart = rollSVG(result.Roll.Values)
	}
	if err := s.tmpl.ExecuteTemplate(w, name, view); err != nil {
		log.Printf("template %s: %v", name, err)
	}
}

func (s *Server) csv(w http.ResponseWriter, r *http.Request) {
	_, result, _, err := s.Compute(false)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="regime-sleeve-backtest.csv"`)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"date", "sleeve", "strategy_return", "rspy_hold_return", "equal_weight_return"})
	for _, p := range result.Points {
		_ = cw.Write([]string{
			p.Date.Format("2006-01-02"),
			string(p.Sleeve),
			fmt.Sprintf("%.8f", p.Ret),
			fmt.Sprintf("%.8f", p.HoldRet),
			fmt.Sprintf("%.8f", p.BlendRet),
		})
	}
	cw.Flush()
}

type pageView struct {
	Err         string
	Book        market.Book
	Result      backtest.Result
	Narrative   string
	Mix         []mixRow
	EquityChart template.HTML
	DrawChart   template.HTML
	RollChart   template.HTML
}

type mixRow struct {
	Name  string
	Days  int
	Pct   float64
	Width string
}

func mixOf(points []backtest.Point) []mixRow {
	counts := map[strategy.Sleeve]int{}
	for _, p := range points {
		counts[p.Sleeve]++
	}
	rows := make([]mixRow, 0, len(strategy.Listings())+1)
	add := func(name strategy.Sleeve) {
		n := counts[name]
		pct := 0.0
		if len(points) > 0 {
			pct = float64(n) / float64(len(points))
		}
		rows = append(rows, mixRow{
			Name:  strategy.Title(name),
			Days:  n,
			Pct:   pct,
			Width: fmt.Sprintf("%.1f%%", pct*100),
		})
	}
	for _, item := range strategy.Listings() {
		add(strategy.Sleeve(item.Key))
	}
	add(strategy.Cash)
	return rows
}

func sleeveTitle(s strategy.Sleeve) string {
	return strategy.Title(s)
}

func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"share": func(v float64) string { return fmt.Sprintf("%.0f%%", v*100) },
		"label": func(s strategy.Sleeve) string { return sleeveTitle(s) },
		"pct":   func(v float64) string { return fmt.Sprintf("%+.1f%%", v*100) },
		"num":   func(v float64) string { return fmt.Sprintf("%.2f", v) },
		"px":    func(v float64) string { return fmt.Sprintf("%.2f", v) },
		"day": func(t time.Time) string {
			if t.IsZero() {
				return "—"
			}
			return t.UTC().Format("2006-01-02")
		},
		"bps": func(v float64) string { return fmt.Sprintf("%.0f bps", v*10000) },
		"neg": func(v float64) bool { return v < 0 },
		"pf": func(m backtest.Metrics) string {
			if !m.HasPF {
				return "—"
			}
			return fmt.Sprintf("%.2f", m.ProfitFactor)
		},
	}
}

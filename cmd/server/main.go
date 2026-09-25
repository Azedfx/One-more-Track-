package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"regime-sleeve/internal/config"
	"regime-sleeve/internal/market"
	"regime-sleeve/internal/web"
)

func main() {
	once := flag.Bool("once", false, "fetch, backtest, print metrics, exit")
	fresh := flag.Bool("fresh", false, "ignore the local price cache")
	flag.Parse()

	cfg := config.Load()
	srv, err := web.New(cfg)
	if err != nil {
		log.Fatal(err)
	}

	if !*once {
		// Server mode: start listening immediately so health checks pass, and
		// warm the price cache in the background. A data hiccup must not stop
		// the service from booting; requests fall back to the bundled seed.
		go func() {
			if _, _, _, err := srv.Compute(*fresh); err != nil {
				log.Printf("initial price load failed, will serve on demand: %v", err)
			}
		}()
		addr := ":" + cfg.Port
		log.Printf("Regime Sleeve listening on http://localhost%s", addr)
		if err := http.ListenAndServe(addr, srv.Handler()); err != nil {
			log.Fatal(err)
		}
		return
	}

	book, result, narrative, err := srv.Compute(*fresh)
	if err != nil {
		log.Fatal(err)
	}
	{
		m := result.Full.Strategy
		o := result.OutSample.Strategy
		in := result.InSample.Strategy
		fmt.Printf("bars %d  tradable %d  %s → %s\n", len(book.Days), m.Days, m.Start.Format("2006-01-02"), m.End.Format("2006-01-02"))
		fmt.Printf("FULL  ret %+.2f%%  cagr %+.2f%%  sharpe %.2f  sortino %.2f  dd %+.2f%%  win %.1f%%  trades %d  tradewin %.1f%%  pf %.2f  turnover %.2f  switches %d\n",
			m.TotalReturn*100, m.CAGR*100, m.Sharpe, m.Sortino, m.MaxDrawdown*100, m.WinRate*100, m.Trades, m.TradeWin*100, m.ProfitFactor, m.Turnover, m.Switches)
		fmt.Printf("IS    ret %+.2f%%  sharpe %.2f  sortino %.2f  dd %+.2f%%  days %d\n", in.TotalReturn*100, in.Sharpe, in.Sortino, in.MaxDrawdown*100, in.Days)
		fmt.Printf("OOS   ret %+.2f%%  sharpe %.2f  sortino %.2f  dd %+.2f%%  days %d\n", o.TotalReturn*100, o.Sharpe, o.Sortino, o.MaxDrawdown*100, o.Days)
		h := result.Full.Hold
		b := result.Full.Blend
		fmt.Printf("HOLD  ret %+.2f%%  sharpe %.2f  dd %+.2f%%\n", h.TotalReturn*100, h.Sharpe, h.MaxDrawdown*100)
		fmt.Printf("BLEND ret %+.2f%%  sharpe %.2f  dd %+.2f%%\n", b.TotalReturn*100, b.Sharpe, b.MaxDrawdown*100)
		fmt.Printf("HARSH sharpe %.2f  dd %+.2f%%\n", result.Harsh.Sharpe, result.Harsh.MaxDrawdown*100)
		fmt.Printf("ROLL  n %d  median %.2f  min %.2f  positive %.1f%%\n", result.Roll.Count, result.Roll.Median, result.Roll.Min, result.Roll.Positive*100)
		fmt.Printf("ALERT %v  %s\n", result.Alert, result.AlertText)
		fmt.Printf("NEXT  %s  %s\n", result.Snap.NextSleeve, narrative)
		fmt.Printf("PPY %.2f  OOS days %d\n", result.PPY, result.OOSDays)
		for _, s := range book.Meta {
			fmt.Printf("SRC %s %s bars %d %s → %s | %s | %s\n", s.Name, s.Symbol, s.Bars, s.Start.Format("2006-01-02"), s.End.Format("2006-01-02"), s.Source, s.Note)
		}
		fmt.Println(book.HistNote)
		fmt.Printf("PROV %s\n", book.Prov.Note)
		oosStart := o.Start
		if market.LiveAsOf(oosStart) {
			fmt.Printf("PROV out-of-sample starts %s, after the rToken launch %s — the held-out window is genuine live rToken data.\n",
				oosStart.Format("2006-01-02"), market.RTokenLaunch.Format("2006-01-02"))
		} else {
			fmt.Printf("PROV out-of-sample starts %s, before the rToken launch %s — part of the held-out window is reference backfill.\n",
				oosStart.Format("2006-01-02"), market.RTokenLaunch.Format("2006-01-02"))
		}
		os.Exit(0)
	}
}

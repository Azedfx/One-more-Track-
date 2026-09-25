package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"regime-sleeve/internal/backtest"
	"regime-sleeve/internal/config"
	"regime-sleeve/internal/strategy"
)

func explain(ctx context.Context, cfg config.Config, snap backtest.Snapshot, spec strategy.Spec) string {
	plain := plainExplain(snap, spec)
	if strings.TrimSpace(cfg.QwenKey) == "" {
		return plain
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	text, err := qwen(ctx, cfg, snap, spec)
	if err != nil || strings.TrimSpace(text) == "" {
		log.Printf("Qwen did not answer in time. The page is showing the rule's own sentence. The strategy does not use Qwen.")
		return plain
	}
	return strings.TrimSpace(text)
}

func plainExplain(snap backtest.Snapshot, spec strategy.Spec) string {
	vol := pct0(snap.RealizedVol)
	eqSMA := spec.EquitySMA
	altSMA := spec.AltSMA
	band := pct0(spec.Band)
	switch snap.NextSleeve {
	case "BTC":
		return fmt.Sprintf("It would hold Bitcoin. The US market is below its %d-day average, swings are still calm at %s a year, and Bitcoin is above its %d-day average.", eqSMA, vol, altSMA)
	case "PAXG":
		return fmt.Sprintf("It would hold gold. US stocks failed the trend or the swing test (%s a year), and gold is above its %d-day average.", vol, altSMA)
	case "CASH", "":
		return fmt.Sprintf("It would hold cash. US stocks failed the trend or the swing test, Bitcoin is not trending up, and gold is not above its %d-day average. Recent swings are %s a year.", altSMA, vol)
	case "rSPY":
		return fmt.Sprintf("It would hold the broad US market. The market is above its %d-day average by more than the %s buffer and swings are calm at %s a year, so the rule stays in stocks.", eqSMA, band, vol)
	default:
		return fmt.Sprintf("It would hold %s. The US market is in a calm uptrend above its %d-day average.", sleeveTitle(snap.NextSleeve), eqSMA)
	}
}

// pct0 renders a fraction as a whole-number percent, e.g. 0.28 -> "28%".
func pct0(v float64) string {
	return fmt.Sprintf("%.0f%%", v*100)
}

func qwen(ctx context.Context, cfg config.Config, snap backtest.Snapshot, spec strategy.Spec) (string, error) {
	prompt := fmt.Sprintf(
		"Write one plain sentence for a non-expert. The holding was already chosen. Do not recommend a trade and do not change it. Use these names only: US stock token, Bitcoin, gold, cash. Holding for the next day: %s. US stock token price %.2f versus its %d-day average %.2f (above average=%t). Recent swings %.0f%% a year, calm=%t, the rule steps aside at %s. Bitcoin %.0f versus its %d-day average %.0f (above=%t). Gold %.0f versus its %d-day average %.0f (above=%t). Say why that holding follows from those facts. No ticker symbols.",
		sleeveTitle(snap.NextSleeve), snap.Equity, spec.EquitySMA, snap.EquitySMA, snap.EquityUp, snap.RealizedVol*100, snap.VolCalm, pct0(spec.VolStress), snap.BTC, spec.AltSMA, snap.BTCSMA, snap.BTCUp, snap.Gold, spec.AltSMA, snap.GoldSMA, snap.GoldUp,
	)
	body, _ := json.Marshal(map[string]any{
		"model":       cfg.QwenModel,
		"temperature": 0,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	})
	url := strings.TrimRight(cfg.QwenBase, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.QwenKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("qwen HTTP %d", resp.StatusCode)
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", err
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("qwen returned no choices")
	}
	return parsed.Choices[0].Message.Content, nil
}

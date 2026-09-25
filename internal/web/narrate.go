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
)

func explain(ctx context.Context, cfg config.Config, snap backtest.Snapshot) string {
	plain := plainExplain(snap)
	if strings.TrimSpace(cfg.QwenKey) == "" {
		return plain
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	text, err := qwen(ctx, cfg, snap)
	if err != nil || strings.TrimSpace(text) == "" {
		log.Printf("Qwen did not answer in time. The page is showing the rule's own sentence. The strategy does not use Qwen.")
		return plain
	}
	return strings.TrimSpace(text)
}

func plainExplain(snap backtest.Snapshot) string {
	vol := fmt.Sprintf("%.0f%%", snap.RealizedVol*100)
	switch snap.NextSleeve {
	case "BTC":
		return fmt.Sprintf("It would hold Bitcoin. The US market is below its 100-day average, swings are still calm at %s a year, and Bitcoin is above its 50-day average.", vol)
	case "PAXG":
		return fmt.Sprintf("It would hold gold. US stocks failed the trend or the swing test (%s a year), and gold is above its 50-day average.", vol)
	case "CASH", "":
		return fmt.Sprintf("It would hold cash. US stocks failed the trend or the swing test, Bitcoin is not trending up, and gold is not above its 50-day average. Recent swings are %s a year.", vol)
	case "rSPY":
		return fmt.Sprintf("It would hold the broad US market. The market is above its 100-day average by more than the 3%% buffer and swings are calm at %s a year, so the rule stays in stocks.", vol)
	default:
		return fmt.Sprintf("It would hold %s. The US market is in a calm uptrend above its 100-day average.", sleeveTitle(snap.NextSleeve))
	}
}

func qwen(ctx context.Context, cfg config.Config, snap backtest.Snapshot) (string, error) {
	prompt := fmt.Sprintf(
		"Write one plain sentence for a non-expert. The holding was already chosen. Do not recommend a trade and do not change it. Use these names only: US stock token, Bitcoin, gold, cash. Holding for the next day: %s. US stock token price %.2f versus its 100-day average %.2f (above average=%t). Recent swings %.0f%% a year, calm=%t, the rule steps aside at 28%%. Bitcoin %.0f versus its 50-day average %.0f (above=%t). Gold %.0f versus its 50-day average %.0f (above=%t). Say why that holding follows from those facts. No ticker symbols.",
		sleeveTitle(snap.NextSleeve), snap.Equity, snap.EquitySMA, snap.EquityUp, snap.RealizedVol*100, snap.VolCalm, snap.BTC, snap.BTCSMA, snap.BTCUp, snap.Gold, snap.GoldSMA, snap.GoldUp,
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

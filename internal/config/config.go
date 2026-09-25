package config

import (
	"bufio"
	"log"
	"os"
	"strings"
)

// Config is process configuration. Secrets stay in the environment or .env.
type Config struct {
	Port      string
	MCPURL    string
	QwenKey   string
	QwenBase  string
	QwenModel string
}

// Load reads .env from the working directory without overriding variables
// that are already set, then applies defaults.
func Load() Config {
	loadDotEnv(".env")
	return Config{
		Port:      getenv("PORT", "8080"),
		MCPURL:    getenv("BITGET_US_MCP_URL", "https://agent.bitget.com/mcp"),
		QwenKey:   os.Getenv("QWEN_API_KEY"),
		QwenBase:  getenv("QWEN_BASE_URL", "https://hackathon.bitgetops.com/v1"),
		QwenModel: getenv("QWEN_MODEL", "qwen3.8-max"),
	}
}

func getenv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		val = strings.Trim(val, `"'`)
		if key == "" || os.Getenv(key) != "" {
			continue
		}
		_ = os.Setenv(key, val)
	}
	if err := sc.Err(); err != nil {
		log.Printf("read %s: %v", path, err)
	}
}

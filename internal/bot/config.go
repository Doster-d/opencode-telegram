package bot

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	TelegramToken string
	OpencodeBase  string
	OpencodeAuth  string
	AllowedIDs    map[int64]bool
	AdminIDs      map[int64]bool
	RedisURL      string
	TelegramMode  string
	Port          string
	SessionPrefix string
	BackendURL    string
}

func LoadConfig() *Config {
	c := &Config{}
	c.TelegramToken = os.Getenv("TELEGRAM_BOT_TOKEN")
	c.OpencodeBase = getenvOr("OPENCODE_BASE_URL", "http://localhost:4096")
	c.OpencodeAuth = os.Getenv("OPENCODE_AUTH_TOKEN")
	c.AllowedIDs = parseIDs(os.Getenv("ALLOWED_TELEGRAM_IDS"))
	c.AdminIDs = parseIDs(os.Getenv("ADMIN_TELEGRAM_IDS"))
	c.RedisURL = os.Getenv("REDIS_URL")
	c.TelegramMode = getenvOr("TELEGRAM_MODE", "polling")
	c.Port = getenvOr("PORT", "3000")
	c.SessionPrefix = getenvOr("SESSION_PREFIX", "oct_")
	c.BackendURL = getenvOr("OCT_BACKEND_URL", "http://localhost:8080")
	return c
}

func parseIDs(s string) map[int64]bool {
	out := make(map[int64]bool)
	s = strings.TrimSpace(s)
	if s == "" {
		return out
	}
	// support space or comma separated
	s = strings.ReplaceAll(s, ",", " ")
	parts := strings.Fields(s)
	for _, p := range parts {
		if id, err := strconv.ParseInt(p, 10, 64); err == nil {
			out[id] = true
		}
	}
	return out
}

func getenvOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

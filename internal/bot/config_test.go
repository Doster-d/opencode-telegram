package bot

import (
	"os"
	"testing"
)

func TestLoadConfig_WithEnvVars(t *testing.T) {
	// backup and restore
	keys := []string{"TELEGRAM_BOT_TOKEN", "OPENCODE_BASE_URL", "OPENCODE_AUTH_TOKEN", "ALLOWED_TELEGRAM_IDS", "ADMIN_TELEGRAM_IDS", "REDIS_URL", "TELEGRAM_MODE", "PORT", "SESSION_PREFIX"}
	old := make(map[string]*string)
	for _, k := range keys {
		v, ok := os.LookupEnv(k)
		if ok {
			vv := v
			old[k] = &vv
		} else {
			old[k] = nil
		}
	}
	defer func() {
		for k, v := range old {
			if v == nil {
				_ = os.Unsetenv(k)
			} else {
				_ = os.Setenv(k, *v)
			}
		}
	}()

	_ = os.Setenv("TELEGRAM_BOT_TOKEN", "tok")
	_ = os.Setenv("OPENCODE_BASE_URL", "http://example.local")
	_ = os.Setenv("OPENCODE_AUTH_TOKEN", "auth123")
	_ = os.Setenv("ALLOWED_TELEGRAM_IDS", "123 456")
	_ = os.Setenv("ADMIN_TELEGRAM_IDS", "999")
	_ = os.Setenv("REDIS_URL", "redis://x")
	_ = os.Setenv("TELEGRAM_MODE", "webhook")
	_ = os.Setenv("PORT", "8080")
	_ = os.Setenv("SESSION_PREFIX", "myprefix_")

	cfg := LoadConfig()

	if cfg.TelegramToken != "tok" {
		t.Fatalf("TelegramToken expected tok, got %q", cfg.TelegramToken)
	}
	if cfg.OpencodeBase != "http://example.local" {
		t.Fatalf("OpencodeBase expected http://example.local, got %q", cfg.OpencodeBase)
	}
	if cfg.OpencodeAuth != "auth123" {
		t.Fatalf("OpencodeAuth expected auth123, got %q", cfg.OpencodeAuth)
	}
	if !cfg.AllowedIDs[123] || !cfg.AllowedIDs[456] {
		t.Fatalf("AllowedIDs parsing failed: %v", cfg.AllowedIDs)
	}
	if !cfg.AdminIDs[999] {
		t.Fatalf("AdminIDs parsing failed: %v", cfg.AdminIDs)
	}
	if cfg.RedisURL != "redis://x" {
		t.Fatalf("RedisURL expected redis://x, got %q", cfg.RedisURL)
	}
	if cfg.TelegramMode != "webhook" {
		t.Fatalf("TelegramMode expected webhook, got %q", cfg.TelegramMode)
	}
	if cfg.Port != "8080" {
		t.Fatalf("Port expected 8080, got %q", cfg.Port)
	}
	if cfg.SessionPrefix != "myprefix_" {
		t.Fatalf("SessionPrefix expected myprefix_, got %q", cfg.SessionPrefix)
	}
}

func TestLoadConfig_Defaults(t *testing.T) {
	// ensure env cleared for relevant keys
	keys := []string{"TELEGRAM_BOT_TOKEN", "OPENCODE_BASE_URL", "OPENCODE_AUTH_TOKEN", "ALLOWED_TELEGRAM_IDS", "ADMIN_TELEGRAM_IDS", "REDIS_URL", "TELEGRAM_MODE", "PORT", "SESSION_PREFIX"}
	saved := make(map[string]*string)
	for _, k := range keys {
		v, ok := os.LookupEnv(k)
		if ok {
			vv := v
			saved[k] = &vv
			_ = os.Unsetenv(k)
		} else {
			saved[k] = nil
		}
	}
	defer func() {
		for k, v := range saved {
			if v == nil {
				_ = os.Unsetenv(k)
			} else {
				_ = os.Setenv(k, *v)
			}
		}
	}()

	cfg := LoadConfig()

	if cfg.TelegramToken != "" {
		t.Fatalf("TelegramToken expected empty, got %q", cfg.TelegramToken)
	}
	if cfg.OpencodeBase != "http://localhost:4096" {
		t.Fatalf("OpencodeBase default mismatch: %q", cfg.OpencodeBase)
	}
	if cfg.OpencodeAuth != "" {
		t.Fatalf("OpencodeAuth expected empty, got %q", cfg.OpencodeAuth)
	}
	if len(cfg.AllowedIDs) != 0 {
		t.Fatalf("AllowedIDs expected empty, got %v", cfg.AllowedIDs)
	}
	if len(cfg.AdminIDs) != 0 {
		t.Fatalf("AdminIDs expected empty, got %v", cfg.AdminIDs)
	}
	if cfg.RedisURL != "" {
		t.Fatalf("RedisURL expected empty, got %q", cfg.RedisURL)
	}
	if cfg.TelegramMode != "polling" {
		t.Fatalf("TelegramMode default mismatch: %q", cfg.TelegramMode)
	}
	if cfg.Port != "3000" {
		t.Fatalf("Port default mismatch: %q", cfg.Port)
	}
	if cfg.SessionPrefix != "oct_" {
		t.Fatalf("SessionPrefix default mismatch: %q", cfg.SessionPrefix)
	}
}

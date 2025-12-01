package main

import (
	"fmt"
	"log"
	"opencode-telegram/internal/bot"
	"opencode-telegram/pkg/store"
	"os"
)

func main() {
	cfg := bot.LoadConfig()
	if cfg.TelegramToken == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN is required")
	}

	// store (memory for now)
	st := store.NewMemoryStore()

	// opencode client
	oc, err := bot.NewOpencodeClient(cfg.OpencodeBase, cfg.OpencodeAuth)
	if err != nil {
		log.Fatalf("opencode client init error: %v", err)
	}

	app, err := bot.NewBotApp(cfg, oc, st)
	if err != nil {
		log.Fatalf("telegram bot init error: %v", err)
	}

	fmt.Println("Starting Telegram bot in", cfg.TelegramMode, "mode")
	// start event listener in background (best-effort)
	go func() {
		if err := app.StartEventListener(); err != nil {
			log.Printf("event listener error: %v", err)
		}
	}()
	if cfg.TelegramMode == "polling" {
		if err := app.StartPolling(); err != nil {
			log.Fatalf("polling error: %v", err)
		}
	} else {
		log.Fatal("webhook mode not implemented yet; use polling for MVP")
		os.Exit(1)
	}
}

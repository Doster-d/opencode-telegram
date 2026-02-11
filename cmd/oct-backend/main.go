package main

import (
	"log"
	"net/http"
	"os"

	"opencode-telegram/internal/backend"
)

func main() {
	addr := os.Getenv("OCT_BACKEND_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	mem := backend.NewMemoryBackend()
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "redis://localhost:6379"
	}
	redisClient, err := backend.NewRealRedisClient(redisURL)
	if err != nil {
		log.Fatalf("redis init error: %v", err)
	}
	queue := backend.NewRedisQueue(redisClient)
	srv := backend.NewServer(mem, queue)
	log.Printf("oct-backend listening on %s", addr)
	if err := http.ListenAndServe(addr, srv); err != nil {
		log.Fatal(err)
	}
}

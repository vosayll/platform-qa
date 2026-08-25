package main

import (
	"log"
	"os"
	"strconv"

	"locali-e2e-engine/config"
	"locali-e2e-engine/pkg/server"
)

func main() {
	cfg := config.LoadFromEnv()

	port := 8080
	if portEnv := os.Getenv("PORT"); portEnv != "" {
		if p, err := strconv.Atoi(portEnv); err == nil && p > 0 {
			port = p
		}
	}

	webDir := "web"
	if customWebDir := os.Getenv("WEB_DIR"); customWebDir != "" {
		webDir = customWebDir
	}

	srv, err := server.NewServer(cfg, webDir)
	if err != nil {
		log.Fatalf("Fatal: failed to initialize E2E Platform server: %v", err)
	}

	if err := srv.Start(port); err != nil {
		log.Fatalf("Server terminated with error: %v", err)
	}
}

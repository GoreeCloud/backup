// GoreeCloud Backup — local-first control API.
// Copyright 2026 GoreeCloud. Licensed under Apache-2.0.
package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/GoreeCloud/backup/internal/backup"
)

func main() {
	cfg, err := backup.ConfigFromEnv()
	if err != nil {
		log.Fatalf("configuration error: %v", err)
	}
	handler, err := backup.NewHandler(cfg)
	if err != nil {
		log.Fatalf("startup error: %v", err)
	}
	server := &http.Server{
		Addr:              cfg.Listen,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      35 * time.Minute,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	log.Printf("GoreeCloud Backup development API listening on %s", cfg.Listen)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Print(err)
		os.Exit(1)
	}
}

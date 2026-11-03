package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/GoreeCloud/backup/internal/control"
)

func main() {
	token := os.Getenv("GOREECLOUD_BACKUP_TOKEN")
	if len(token) < 24 {
		log.Fatal("GOREECLOUD_BACKUP_TOKEN requires a random secret of at least 24 characters")
	}
	address := os.Getenv("GOREECLOUD_BACKUP_ADDR")
	if address == "" {
		address = "127.0.0.1:8787"
	}
	if err := control.ValidateAddress(address); err != nil {
		log.Fatal(err)
	}
	var handler http.Handler
	if stateDir := os.Getenv("GOREECLOUD_BACKUP_STATE_DIR"); stateDir != "" {
		var err error
		handler, err = control.NewServerWithState(token, control.OSExecutor{}, stateDir)
		if err != nil {
			log.Fatalf("private job history unavailable: %v", err)
		}
	} else {
		handler = control.NewServer(token, control.OSExecutor{})
	}
	server := &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      20 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	log.Printf("GoreeCloud Backup development build listening on http://%s", address)
	log.Fatal(server.ListenAndServe())
}

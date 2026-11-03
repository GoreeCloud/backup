// GoreeCloud Backup trigger: a bounded, loopback-only snapshot job client.
// Intended for an opt-in local Linux timer; does not install or enable timers.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/GoreeCloud/backup/internal/trigger"
)

func main() {
	source := flag.String("source", "", "absolute local source directory to back up")
	address := flag.String("address", "127.0.0.1:8787", "loopback GoreeCloud Backup HTTP address")
	tokenFile := flag.String("token-file", "", "private (0600) token file; never put token on command line")
	maxWait := flag.Duration("max-wait", 35*time.Minute, "maximum time to await a backup result")
	flag.Parse()
	log.SetFlags(0)
	if *tokenFile == "" || *source == "" || *maxWait < time.Minute || *maxWait > 45*time.Minute || flag.NArg() != 0 {
		log.Fatal("require -source, -token-file and valid -max-wait (1m–45m); no positional args")
	}
	token, err := trigger.LoadToken(*tokenFile)
	if err != nil {
		log.Fatalf("invalid private token file: %v", err)
	}
	client := trigger.Client{BaseURL: "http://" + *address, Token: token}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, *maxWait)
	defer cancel()
	id, err := client.SubmitAndWait(ctx, *source)
	if id != "" {
		fmt.Printf("Snapshot job: %s\n", id)
	}
	if err != nil {
		log.Fatalf("snapshot did not complete successfully: %v", err)
	}
}

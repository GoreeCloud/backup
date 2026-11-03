// GoreeCloud Backup scheduler: opt-in Linux one-shot UTC recurrence controller.
// It never installs/enables a timer and does not start a background daemon.
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

	"github.com/GoreeCloud/backup/internal/scheduler"
	"github.com/GoreeCloud/backup/internal/trigger"
)

func main() {
	configFile := flag.String("config", "", "absolute private (0600) JSON scheduling policy path")
	stateDir := flag.String("state-dir", "", "absolute private (0700) schedule ledger directory")
	tokenFile := flag.String("token-file", "", "absolute private (0600) API bearer token path")
	address := flag.String("address", "127.0.0.1:8787", "local loopback management API")
	maxWait := flag.Duration("max-wait", 35*time.Minute, "timeout per snapshot and optional verification (1m–45m)")
	check := flag.Bool("check-config", false, "validate private policy file only, never run jobs")
	runDue := flag.Bool("run-due", false, "claim and run eligible policy slots once")
	flag.Parse()
	log.SetFlags(0)
	if flag.NArg() != 0 || (*check == *runDue) || *configFile == "" {
		log.Fatal("require -config and exactly one of -check-config or -run-due")
	}
	cfg, err := scheduler.LoadConfiguration(*configFile)
	if err != nil {
		log.Fatalf("schedule config rejected: %v", err)
	}
	if *check {
		enabled := 0
		for _, p := range cfg.Policies {
			if p.Enabled {
				enabled++
			}
		}
		fmt.Printf("Schedule configuration valid: %d policies, %d explicitly enabled\n",
			len(cfg.Policies), enabled)
		return
	}
	if *stateDir == "" || *tokenFile == "" || *maxWait < time.Minute || *maxWait > 45*time.Minute {
		log.Fatal("require -state-dir, -token-file and -max-wait between 1m and 45m")
	}
	token, err := trigger.LoadToken(*tokenFile)
	if err != nil {
		log.Fatalf("private API token unavailable: %v", err)
	}
	store, err := scheduler.OpenStore(*stateDir)
	if err != nil {
		log.Fatalf("private schedule state unavailable: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			log.Printf("schedule state lock release failed: %v", err)
		}
	}()
	client := trigger.Client{BaseURL: "http://" + *address, Token: token}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	outcomes, err := scheduler.RunDue(ctx, cfg, store, time.Now,
		func(ctx context.Context, p scheduler.Policy) (string, string, error) {
			limited, cancel := context.WithTimeout(ctx, *maxWait)
			defer cancel()
			return client.RunBackupAndVerify(limited, p.Source, p.VerifyAfter, p.AcknowledgeFull)
		})
	for _, result := range outcomes {
		fmt.Printf("Policy %s: %s\n", result.PolicyID, result.Outcome)
	}
	if len(outcomes) == 0 {
		fmt.Println("No enabled policy due within its UTC scheduling window.")
	}
	if err != nil {
		log.Fatalf("one or more eligible schedule jobs failed: %v", err)
	}
}

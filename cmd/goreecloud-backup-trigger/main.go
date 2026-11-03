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
	verifyAfter := flag.String("verify-after", "none", "optional post-backup verification: none, structure, full")
	ackFull := flag.Bool("acknowledge-full-verification-cost", false, "explicitly allow full verification of all repository data, including cloud egress costs")
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
	snapshotID, verificationID, err := client.RunBackupAndVerify(ctx, *source, *verifyAfter, *ackFull)
	if snapshotID != "" {
		fmt.Printf("Snapshot job: %s\n", snapshotID)
	}
	if verificationID != "" {
		fmt.Printf("Integrity verification job: %s\n", verificationID)
	}
	if err != nil {
		log.Fatalf("snapshot or requested verification did not complete successfully: %v", err)
	}
}

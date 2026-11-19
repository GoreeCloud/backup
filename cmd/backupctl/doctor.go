package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// Doctor is deliberately metadata-only. It never invokes POST operations,
// backup creation, repository full-content verification, or remote quota APIs.
// A passing preflight is NOT an independently successful restore.
type doctorInfo struct {
	Name           string
	ReadOnly       *bool
	CanSnapshot    *bool
	CanVerify      *bool
	RestoreEnabled *bool
}

type doctorStatus struct {
	Connected *bool
}

type doctorReport struct {
	ReadOnly        bool
	Connected       bool
	VisibleSnapshot bool
	CanSnapshot     bool
	CanVerify       bool
	RestoreEnabled  bool
}

func executeDoctor(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	timeout := flags.Duration("timeout", 30*time.Second, "maximum metadata preflight duration")
	if flags.Parse(args) != nil || len(flags.Args()) != 0 ||
		*timeout < 3*time.Second || *timeout > 2*time.Minute {
		fmt.Fprintln(stderr, "Usage: backupctl doctor [--timeout=30s]; timeout must be 3s..2m")
		return 2
	}
	endpoint := os.Getenv("GCB_API_URL")
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	if validateEndpoint(endpoint) != nil {
		fmt.Fprintln(stderr, "Invalid backup API endpoint: numeric 127.0.0.1 HTTP only")
		return 2
	}
	token, err := readTokenFile(os.Getenv("GCB_API_TOKEN_FILE"))
	if err != nil {
		fmt.Fprintln(stderr, "Cannot open required private GCB_API_TOKEN_FILE:", err)
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	report, err := doctorMetadata(ctx, endpoint, token)
	if err != nil {
		fmt.Fprintln(stderr, "Local preflight could not verify repository metadata:", err)
		return 1
	}
	scope := "administrator"
	if report.ReadOnly {
		scope = "read-only viewer"
	}
	fmt.Fprintf(stdout, "GoreeCloud Backup local metadata preflight\nCredential scope: %s\n", scope)
	if report.Connected {
		fmt.Fprintln(stdout, "Repository connection: connected")
	} else {
		fmt.Fprintln(stdout, "Repository connection: unavailable")
	}
	if report.VisibleSnapshot {
		fmt.Fprintln(stdout, "Snapshot listing: at least one visible snapshot")
	} else {
		fmt.Fprintln(stdout, "Snapshot listing: no visible snapshots")
	}
	fmt.Fprintf(stdout, "Snapshot writes permitted by token: %t\n", report.CanSnapshot)
	fmt.Fprintf(stdout, "Full-file verification permitted by token: %t\n", report.CanVerify)
	fmt.Fprintf(stdout, "Contained restore permitted and configured for token: %t\n", report.RestoreEnabled)
	fmt.Fprintln(stdout, "No archived file contents were verified or restored. Perform an independent recovery drill.")
	if !report.Connected || !report.VisibleSnapshot {
		fmt.Fprintln(stderr, "Preflight NOT ready: repository connection or visible recovery point is missing.")
		return 1
	}
	return 0
}

func doctorGET(ctx context.Context, endpoint, token, route string, result any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+route, nil)
	if err != nil {
		return errors.New("cannot construct local metadata request")
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/json")
	response, err := newLocalClient().Do(request)
	if err != nil {
		return errors.New("local backup API unavailable or timed out")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("local metadata request returned HTTP %d", response.StatusCode)
	}
	if decodeBoundedJSON(response.Body, result) != nil {
		return errors.New("local backup API returned malformed or oversized metadata")
	}
	return nil
}

func doctorMetadata(ctx context.Context, endpoint, token string) (doctorReport, error) {
	var empty doctorReport
	var info doctorInfo
	if err := doctorGET(ctx, endpoint, token, "/api/v1/info", &info); err != nil {
		return empty, err
	}
	if info.Name != "GoreeCloud Backup" || info.ReadOnly == nil ||
		info.CanSnapshot == nil || info.CanVerify == nil || info.RestoreEnabled == nil {
		return empty, errors.New("unknown API identity or incomplete capability metadata")
	}
	if *info.ReadOnly && (*info.CanSnapshot || *info.CanVerify || *info.RestoreEnabled) {
		return empty, errors.New("read-only scope claims privileged operations")
	}
	var state doctorStatus
	if err := doctorGET(ctx, endpoint, token, "/api/v1/status", &state); err != nil {
		return empty, err
	}
	if state.Connected == nil {
		return empty, errors.New("repository connection state was not supplied")
	}
	report := doctorReport{
		ReadOnly: *info.ReadOnly, Connected: *state.Connected,
		CanSnapshot: *info.CanSnapshot, CanVerify: *info.CanVerify,
		RestoreEnabled: *info.RestoreEnabled,
	}
	// Do not query snapshots when the repository is already unavailable.
	if report.Connected {
		snapshots, err := listSnapshots(ctx, endpoint, token, 1)
		if err != nil {
			return empty, err
		}
		report.VisibleSnapshot = len(snapshots) > 0
	}
	return report, nil
}

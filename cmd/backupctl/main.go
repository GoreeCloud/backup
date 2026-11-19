// GoreeCloud Backup's opt-in local command client for audited Linux timers.
// Copyright 2026 GoreeCloud. Licensed under Apache-2.0.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const defaultEndpoint = "http://127.0.0.1:8787"

func main() { os.Exit(execute(os.Args[1:], os.Stdout, os.Stderr)) }

func execute(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "verify" {
		return executeVerify(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "list" {
		return executeList(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "doctor" {
		return executeDoctor(args[1:], stdout, stderr)
	}
	if len(args) == 0 || args[0] != "snapshot" {
		fmt.Fprintln(stderr, "Usage: backupctl snapshot --source=Documents [--timeout=30m]")
		fmt.Fprintln(stderr, "       backupctl verify --accept-provider-costs [--timeout=30m]")
		fmt.Fprintln(stderr, "       backupctl list [--limit=50] [--timeout=30s]")
		fmt.Fprintln(stderr, "       backupctl doctor [--timeout=30s]")
		return 2
	}
	flags := flag.NewFlagSet("snapshot", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	source := flags.String("source", "", "relative source directory below server allowlist")
	timeout := flags.Duration("timeout", 30*time.Minute, "request deadline")
	if err := flags.Parse(args[1:]); err != nil || len(flags.Args()) != 0 {
		fmt.Fprintln(stderr, "Invalid flags; expected --source and optional --timeout")
		return 2
	}
	if err := validateSource(*source); err != nil {
		fmt.Fprintln(stderr, "Invalid source: must be a safe relative subdirectory")
		return 2
	}
	if *timeout < 10*time.Second || *timeout > 35*time.Minute {
		fmt.Fprintln(stderr, "Timeout must be between 10 seconds and 35 minutes")
		return 2
	}
	endpoint := os.Getenv("GCB_API_URL")
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	if err := validateEndpoint(endpoint); err != nil {
		fmt.Fprintln(stderr, "Invalid backup API endpoint: only numeric loopback HTTP is supported")
		return 2
	}
	token, err := readTokenFile(os.Getenv("GCB_API_TOKEN_FILE"))
	if err != nil {
		fmt.Fprintln(stderr, "Unable to read a secure GCB_API_TOKEN_FILE:", err)
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	if err := snapshot(ctx, endpoint, *source, token); err != nil {
		fmt.Fprintln(stderr, "Snapshot unsuccessful:", err)
		return 1
	}
	fmt.Fprintln(stdout, "Kopia snapshot command completed; verify repository contents and recovery readiness separately.")
	return 0
}
func validateSource(source string) error {
	if source == "" || len(source) > 512 || filepath.IsAbs(source) || strings.ContainsAny(source, "\r\n\x00") {
		return errors.New("invalid relative source")
	}
	// No path traversal or alternate Windows separators even on Linux.
	if strings.Contains(source, "\\") {
		return errors.New("invalid separator")
	}
	for _, part := range strings.Split(source, "/") {
		if part == "" || part == "." || part == ".." {
			return errors.New("invalid path segment")
		}
	}
	return nil
}
func validateEndpoint(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed == nil || parsed.Scheme != "http" || parsed.User != nil ||
		parsed.Hostname() != "127.0.0.1" || parsed.Path != "" || parsed.RawQuery != "" ||
		parsed.Fragment != "" || parsed.Opaque != "" {
		return errors.New("endpoint must use numeric loopback HTTP with no path/query")
	}
	_, port, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		return errors.New("endpoint port required")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return errors.New("invalid port")
	}
	return nil
}

// readTokenFile pins the configured parent directory before opening the
// credential, and validates the same regular, private file before and after
// reading. A file/symlink substituted between checks cannot redirect reads
// outside the private root or silently substitute another credential.
func readTokenFile(name string) (string, error) {
	if name == "" || !filepath.IsAbs(name) {
		return "", errors.New("token file must be an absolute path")
	}
	parent, err := os.OpenRoot(filepath.Dir(name))
	if err != nil {
		return "", errors.New("token parent directory is unavailable")
	}
	defer parent.Close()
	return readTokenFromRoot(parent, filepath.Base(name))
}

func readTokenFromRoot(parent *os.Root, leaf string) (string, error) {
	before, err := parent.Lstat(leaf)
	if err != nil {
		return "", errors.New("token file is unavailable")
	}
	if !before.Mode().IsRegular() || before.Mode().Perm()&0077 != 0 {
		return "", errors.New("token file must be a private regular file (0600 or stricter)")
	}
	if before.Size() < 32 || before.Size() > 4096 {
		return "", errors.New("invalid token file size")
	}
	file, err := parent.Open(leaf)
	if err != nil {
		return "", errors.New("cannot open private token file")
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		return "", errors.New("token file identity changed while opening")
	}
	// Read from this held descriptor, not from the pathname re-resolved
	// after Lstat. 4097 bytes detects oversize without unbounded allocation.
	content, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil || len(content) < 32 || len(content) > 4096 {
		return "", errors.New("cannot read private token content within size limit")
	}
	afterFile, fileErr := file.Stat()
	afterPath, pathErr := parent.Lstat(leaf)
	if fileErr != nil || pathErr != nil || !os.SameFile(before, afterFile) ||
		!os.SameFile(before, afterPath) || afterFile.Mode().Perm()&0077 != 0 ||
		afterFile.Size() != int64(len(content)) || before.Size() != afterFile.Size() {
		return "", errors.New("token file identity, permissions or size changed while reading")
	}
	token := strings.TrimSpace(string(content))
	if len(token) < 32 || len(token) > 4096 || strings.ContainsAny(token, " \t\n\r\x00") {
		return "", errors.New("invalid token length or characters")
	}
	return token, nil
}

func snapshot(ctx context.Context, endpoint, source, token string) error {
	body, err := json.Marshal(map[string]string{"source": source})
	if err != nil {
		return errors.New("cannot encode request")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/api/v1/snapshots", bytes.NewReader(body))
	if err != nil {
		return errors.New("cannot build request")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := newLocalClient().Do(req)
	if err != nil {
		return errors.New("local backup service unreachable or timed out")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("server returned HTTP %d", resp.StatusCode)
	}
	var result struct {
		Accepted bool `json:"accepted"`
	}
	if err := decodeBoundedJSON(resp.Body, &result); err != nil || !result.Accepted {
		return errors.New("unexpected backup API response")
	}
	return nil
}

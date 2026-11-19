package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// A full-file-content read can be expensive on cloud providers. The operator
// must explicitly acknowledge those charges for every CLI invocation.
func executeVerify(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	accept := flags.Bool("accept-provider-costs", false, "acknowledge full-file cloud read and transfer costs")
	timeout := flags.Duration("timeout", 30*time.Minute, "request deadline")
	if err := flags.Parse(args); err != nil || len(flags.Args()) != 0 {
		fmt.Fprintln(stderr, "Invalid arguments; expected verify --accept-provider-costs [--timeout=30m]")
		return 2
	}
	if !*accept {
		fmt.Fprintln(stderr, "Explicit --accept-provider-costs is required; this operation reads all backed-up file contents.")
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
	result, err := verifyArchive(ctx, endpoint, token)
	if err != nil {
		fmt.Fprintln(stderr, "Archive verification unsuccessful:", err)
		return 1
	}
	fmt.Fprintf(stdout, "Verified %d unique archived files (%d bytes read). Independently test restoration before relying on this backup.\n", result.FilesRead, result.BytesRead)
	return 0
}

type verificationReport struct {
	Verified  bool  `json:"verified"`
	FilesRead int64 `json:"filesRead"`
	BytesRead int64 `json:"bytesRead"`
}

func newLocalClient() *http.Client {
	// Never follow redirects with bearer credentials or consult environment
	// proxies; this is a numeric-loopback-only control plane.
	return &http.Client{
		Transport:     &http.Transport{Proxy: nil, DisableKeepAlives: true, MaxResponseHeaderBytes: 16 << 10},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// decodeBoundedJSON rejects oversized responses even if a valid JSON object
// is followed by unlimited whitespace. Trailing protocol data fails closed.
func decodeBoundedJSON(reader io.Reader, target any) error {
	raw, err := io.ReadAll(io.LimitReader(reader, 4097))
	if err != nil || len(raw) == 0 || len(raw) > 4096 {
		return errors.New("local API response is empty, invalid or exceeds 4096 bytes")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(target); err != nil {
		return errors.New("invalid local API JSON")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return errors.New("unexpected trailing JSON data")
	}
	return nil
}

func verifyArchive(ctx context.Context, endpoint, token string) (verificationReport, error) {
	var empty verificationReport
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/api/v1/verify", nil)
	if err != nil {
		return empty, errors.New("cannot create local verification request")
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/json")
	response, err := newLocalClient().Do(request)
	if err != nil {
		return empty, errors.New("local backup service unreachable or timed out")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return empty, fmt.Errorf("server returned HTTP %d", response.StatusCode)
	}
	var result verificationReport
	if err := decodeBoundedJSON(response.Body, &result); err != nil || !result.Verified || result.FilesRead < 1 || result.BytesRead < 0 {
		return empty, errors.New("unexpected backup integrity response")
	}
	return result, nil
}

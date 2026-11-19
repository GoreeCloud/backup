package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"sort"
	"strings"
	"time"
	"unicode"
)

// snapshot-list records can contain host/user names, full source paths,
// object IDs and tags. Keep CLI output deliberately small and privacy-minded.
type localSnapshot struct {
	ID        string `json:"id"`
	StartTime string `json:"startTime"`
	Source    struct {
		Path string `json:"path"`
	} `json:"source"`
}

type snapshotLine struct {
	ID        string
	Timestamp string
	Label     string
}

func executeList(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("list", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	limit := flags.Int("limit", 50, "maximum recent snapshot records")
	timeout := flags.Duration("timeout", 30*time.Second, "request deadline")
	if flags.Parse(args) != nil || len(flags.Args()) != 0 || *limit < 1 || *limit > 100 {
		fmt.Fprintln(stderr, "Usage: backupctl list [--limit=50] [--timeout=30s]; limit must be 1..100")
		return 2
	}
	if *timeout < 3*time.Second || *timeout > 2*time.Minute {
		fmt.Fprintln(stderr, "Timeout must be between 3 seconds and 2 minutes")
		return 2
	}
	endpoint := os.Getenv("GCB_API_URL")
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	if validateEndpoint(endpoint) != nil {
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
	entries, err := listSnapshots(ctx, endpoint, token, *limit)
	if err != nil {
		fmt.Fprintln(stderr, "Snapshot listing unsuccessful:", err)
		return 1
	}
	if len(entries) == 0 {
		fmt.Fprintln(stdout, "No snapshots found")
		return 0
	}
	fmt.Fprintln(stdout, "SNAPSHOT ID\tTIMESTAMP (UTC)\tSOURCE LABEL")
	for _, entry := range entries {
		fmt.Fprintf(stdout, "%s\t%s\t%s\n", entry.ID, entry.Timestamp, entry.Label)
	}
	return 0
}

func safeSnapshotDisplay(value string) string {
	if len(value) > 4096 {
		return "Protected snapshot"
	}
	normalized := strings.ReplaceAll(value, "\\", "/")
	label := path.Base(strings.TrimRight(normalized, "/"))
	if label == "" || label == "." || strings.HasSuffix(label, ":") {
		label = "Protected snapshot"
	}
	// Reject bidirectional overrides and HTML-looking path components rather
	// than displaying spoofed names in terminal/log output.
	for _, r := range label {
		if r == '<' || r == '>' || (r >= '\u202a' && r <= '\u202e') ||
			(r >= '\u2066' && r <= '\u2069') {
			return "Protected snapshot"
		}
	}
	label = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, label)
	if label == "" {
		label = "Protected snapshot"
	}
	letters := []rune(label)
	if len(letters) > 80 {
		label = string(letters[:80])
	}
	return label
}

func validSnapshotID(value string) bool {
	if len(value) < 8 || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

func listSnapshots(ctx context.Context, endpoint, token string, limit int) ([]snapshotLine, error) {
	if limit < 1 || limit > 100 {
		return nil, errors.New("invalid snapshot list size")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/api/v1/snapshots", nil)
	if err != nil {
		return nil, errors.New("cannot construct local snapshot-list request")
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/json")
	response, err := newLocalClient().Do(request)
	if err != nil {
		return nil, errors.New("local backup service unavailable or timed out")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned HTTP %d", response.StatusCode)
	}
	const maxJSON = 2 << 20
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxJSON+1))
	if err != nil || len(raw) == 0 || len(raw) > maxJSON {
		return nil, errors.New("snapshot listing exceeded the maximum response size or could not be read")
	}
	var snapshots []localSnapshot
	if json.Unmarshal(raw, &snapshots) != nil || snapshots == nil {
		return nil, errors.New("malformed snapshot listing")
	}
	// Kopia's listing order is not a recovery-freshness guarantee.
	// Sort by actual reported dates and keep ties stable (newer list entries
	// first). Undated records follow dated records, and repeated IDs are
	// collapsed to their best dated record.
	type datedSnapshot struct {
		snapshotLine
		instant time.Time
		index   int
	}
	all := make([]datedSnapshot, 0, len(snapshots))
	for index, item := range snapshots {
		if !validSnapshotID(item.ID) {
			continue
		}
		stamp := "unknown"
		var instant time.Time
		if parsed, err := time.Parse(time.RFC3339Nano, item.StartTime); err == nil {
			instant = parsed.UTC()
			stamp = instant.Format(time.RFC3339)
		}
		all = append(all, datedSnapshot{
			snapshotLine: snapshotLine{
				ID: item.ID, Timestamp: stamp,
				Label: safeSnapshotDisplay(item.Source.Path),
			},
			instant: instant,
			index:   index,
		})
	}
	sort.SliceStable(all, func(i, j int) bool {
		a, b := all[i], all[j]
		if a.instant.IsZero() != b.instant.IsZero() {
			return !a.instant.IsZero()
		}
		if !a.instant.Equal(b.instant) {
			return a.instant.After(b.instant)
		}
		return a.index > b.index
	})
	result := make([]snapshotLine, 0, limit)
	seen := make(map[string]bool)
	for _, candidate := range all {
		if seen[candidate.ID] {
			continue
		}
		seen[candidate.ID] = true
		result = append(result, candidate.snapshotLine)
		if len(result) >= limit {
			break
		}
	}
	return result, nil
}

package trigger

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/GoreeCloud/backup/internal/control"
)

const maxTokenFileBytes = 4096

// LoadToken reads an operator-managed, private regular token file.
// It never creates, logs, or persists the management secret.
func LoadToken(filename string) (string, error) {
	if !filepath.IsAbs(filename) {
		return "", errors.New("token file path must be absolute")
	}
	info, err := os.Lstat(filename)
	if err != nil {
		return "", fmt.Errorf("cannot inspect token file: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 ||
		info.Mode().Perm()&0400 == 0 || info.Size() > maxTokenFileBytes {
		return "", errors.New("token must be a private readable regular file, max 4096 bytes")
	}
	file, err := os.Open(filename)
	if err != nil {
		return "", fmt.Errorf("cannot open token file: %w", err)
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return "", err
	}
	if !opened.Mode().IsRegular() || opened.Mode().Perm()&0077 != 0 ||
		opened.Mode().Perm()&0400 == 0 || opened.Size() > maxTokenFileBytes ||
		!os.SameFile(info, opened) {
		return "", errors.New("token file changed or is not private")
	}
	contents, err := io.ReadAll(io.LimitReader(file, maxTokenFileBytes+1))
	if err != nil {
		return "", fmt.Errorf("cannot read token file: %w", err)
	}
	if len(contents) > maxTokenFileBytes {
		return "", errors.New("token file exceeds size limit")
	}
	token := strings.TrimSuffix(string(contents), "\n")
	if len(token) < 24 || strings.TrimSpace(token) != token || strings.ContainsAny(token, "\r\n\x00") {
		return "", errors.New("invalid token file contents")
	}
	return token, nil
}

// Client submits only a non-destructive snapshot job and polls for its final
// result. The requested URL must remain on an explicit HTTP loopback address.
type Client struct {
	BaseURL      string
	Token        string
	HTTP         *http.Client
	PollInterval time.Duration
}

type jobResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

func (c Client) checkedURL() (string, error) {
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Scheme != "http" || u.User != nil ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("service URL must be HTTP loopback host:port only")
	}
	if err := control.ValidateAddress(u.Host); err != nil {
		return "", err
	}
	if len(c.Token) < 24 || strings.ContainsAny(c.Token, "\r\n\x00") {
		return "", errors.New("invalid API token")
	}
	return strings.TrimSuffix(c.BaseURL, "/"), nil
}

func (c Client) do(ctx context.Context, method, endpoint string, body []byte) (*jobResponse, int, error) {
	base, err := c.checkedURL()
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, method, base+endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	// Never forward a bearer token to a redirected host.
	safeClient := *httpClient
	safeClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := safeClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, nil
	}
	var job jobResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8192)).Decode(&job); err != nil {
		return nil, resp.StatusCode, errors.New("service returned invalid JSON")
	}
	if len(job.ID) != 24 {
		return nil, resp.StatusCode, errors.New("service returned invalid job ID")
	}
	if bytes, err := hex.DecodeString(job.ID); err != nil || len(bytes) != 12 {
		return nil, resp.StatusCode, errors.New("service returned invalid job ID")
	}
	return &job, resp.StatusCode, nil
}

func (c Client) SubmitAndWait(ctx context.Context, source string) (string, error) {
	if _, err := control.BuildCommand(control.Request{Type: "backup", Source: source}); err != nil {
		return "", fmt.Errorf("invalid backup source: %w", err)
	}
	return c.submitAndWait(ctx, control.Request{Type: "backup", Source: source})
}

// submitAndWait uses the same guarded API for a backup or explicitly selected
// verification job. The caller must validate the operation and confirmation.
func (c Client) submitAndWait(ctx context.Context, request control.Request) (string, error) {
	encoded, err := json.Marshal(request)
	if err != nil {
		return "", err
	}
	job, code, err := c.do(ctx, http.MethodPost, "/api/v1/jobs", encoded)
	if err != nil {
		return "", err
	}
	if code != http.StatusAccepted {
		return "", fmt.Errorf("%s request rejected (HTTP %d)", request.Type, code)
	}
	id := job.ID
	interval := c.PollInterval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		status, code, err := c.do(ctx, http.MethodGet, "/api/v1/jobs/"+id, nil)
		if err != nil {
			return id, err
		}
		if code != http.StatusOK {
			return id, fmt.Errorf("job status unavailable (HTTP %d)", code)
		}
		switch status.Status {
		case "completed":
			return id, nil
		case "cancelled", "interrupted", "failed":
			return id, fmt.Errorf("%s job ended in status %s; inspect local management history", request.Type, status.Status)
		case "queued", "running", "cancelling":
		default:
			return id, fmt.Errorf("%s job returned unexpected status", request.Type)
		}
		select {
		case <-ctx.Done():
			return id, ctx.Err()
		case <-ticker.C:
		}
	}
}

// RunBackupAndVerify performs a snapshot first, then an optionally requested
// integrity check. A failed snapshot never starts verification. The full mode
// requires a separate explicit acknowledgment because it may download all
// stored data and incur cloud egress costs. The default mode is "none".
func (c Client) RunBackupAndVerify(ctx context.Context, source, mode string, acknowledgeFullCost bool) (string, string, error) {
	switch mode {
	case "none", "structure":
		if acknowledgeFullCost {
			return "", "", errors.New("full-verification cost acknowledgment is only valid for full mode")
		}
	case "full":
		if !acknowledgeFullCost {
			return "", "", errors.New("full verification requires explicit cost acknowledgment")
		}
	default:
		return "", "", errors.New("verification mode must be none, structure, or full")
	}

	// Reject invalid sources before any API job is admitted.
	if _, err := control.BuildCommand(control.Request{Type: "backup", Source: source}); err != nil {
		return "", "", fmt.Errorf("invalid backup source: %w", err)
	}
	snapshotID, err := c.SubmitAndWait(ctx, source)
	if err != nil {
		return snapshotID, "", err
	}
	if mode == "none" {
		return snapshotID, "", nil
	}
	verification := control.Request{Type: "verify-structure"}
	if mode == "full" {
		verification.Type = "verify-full"
		verification.Confirm = "VERIFY ALL BACKUPS"
	}
	verificationID, err := c.submitAndWait(ctx, verification)
	return snapshotID, verificationID, err
}

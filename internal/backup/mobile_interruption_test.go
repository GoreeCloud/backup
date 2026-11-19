package backup

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These tests use only throwaway sources and a deliberately nonexistent Kopia
// command. A failed/oversized HTTP stream must never reach snapshot creation.
func newAbortTestAPI(t *testing.T) (http.Handler, string) {
	t.Helper()
	source := t.TempDir()
	ingest := filepath.Join(t.TempDir(), "private-ingest")
	if err := os.Mkdir(ingest, 0700); err != nil {
		t.Fatal(err)
	}
	handler, err := NewHandler(Config{
		Token: testToken, SourceRoot: source, MobileIngestRoot: ingest, KopiaBin: "/not-installed/kopia",
	})
	if err != nil {
		t.Fatal(err)
	}
	return handler, ingest
}

func requestForUpload(body io.Reader, length int64) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/mobile/files", body)
	request.ContentLength = length
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set("Content-Type", "application/octet-stream")
	request.Header.Set("X-GCB-Filename-B64", base64.RawURLEncoding.EncodeToString([]byte("selected-document.txt")))
	request.Header.Set("X-GCB-Confirm-Upload", "true")
	return request
}

func assertNoAbandonedFiles(t *testing.T, ingest string) {
	t.Helper()
	entries, err := os.ReadDir(ingest)
	if err != nil || len(entries) != 0 {
		t.Fatalf("aborted plaintext staging persisted: %v %v", entries, err)
	}
}

type interruptedSource struct{ delivered bool }

func (r *interruptedSource) Read(p []byte) (int, error) {
	if r.delivered {
		return 0, io.ErrUnexpectedEOF
	}
	r.delivered = true
	return copy(p, []byte("synthetic partially transferred file content")), nil
}

type zeroSource struct{}

func (zeroSource) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

func TestMobileUploadMidstreamTransportFailureCleansPlaintext(t *testing.T) {
	api, ingest := newAbortTestAPI(t)
	request := requestForUpload(&interruptedSource{}, -1)
	recorder := httptest.NewRecorder()
	api.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Errorf("interrupted stream got HTTP %d rather than 400", recorder.Code)
	}
	assertNoAbandonedFiles(t, ingest)
}

func TestMobileUploadKnownLengthMismatchRejected(t *testing.T) {
	api, ingest := newAbortTestAPI(t)
	for _, tc := range []struct {
		body     string
		declared int64
	}{
		{"short", 35},
		{"longer than declared", 3},
	} {
		request := requestForUpload(strings.NewReader(tc.body), tc.declared)
		recorder := httptest.NewRecorder()
		api.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("declared %d actual %d: got HTTP %d rather than 400", tc.declared, len(tc.body), recorder.Code)
		}
		assertNoAbandonedFiles(t, ingest)
	}
}

func TestMobileUploadUnknownLengthOverLimitCleansPlaintext(t *testing.T) {
	api, ingest := newAbortTestAPI(t)
	input := io.LimitReader(zeroSource{}, maxMobileFileBytes+1)
	request := requestForUpload(input, -1)
	recorder := httptest.NewRecorder()
	api.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("over-limit chunked stream got HTTP %d rather than 413", recorder.Code)
	}
	assertNoAbandonedFiles(t, ingest)
}

func TestMobileUploadEmptyContentRejectedWithoutStaging(t *testing.T) {
	api, ingest := newAbortTestAPI(t)
	request := requestForUpload(strings.NewReader(""), 0)
	recorder := httptest.NewRecorder()
	api.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Errorf("empty selected file got HTTP %d rather than 400", recorder.Code)
	}
	assertNoAbandonedFiles(t, ingest)
}

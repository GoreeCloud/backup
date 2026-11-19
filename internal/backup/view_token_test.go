package backup

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

const viewTestToken = "viewer-only-development-test-token-0123456789"

func TestViewerCanReadMetadataButCannotMutate(t *testing.T) {
	source := t.TempDir()
	h, err := NewHandler(Config{Token: testToken, ViewToken: viewTestToken, SourceRoot: source,
		KopiaBin: "/missing-kopia", RcloneBin: "/missing-rclone"})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/v1/info", "/api/v1/status", "/api/v1/snapshots", "/api/v1/remotes"} {
		request := httptest.NewRequest("GET", path, nil)
		request.Header.Set("Authorization", "Bearer "+viewTestToken)
		recorder := httptest.NewRecorder()
		h.ServeHTTP(recorder, request)
		if recorder.Code == http.StatusUnauthorized {
			t.Errorf("viewer denied safe read %s", path)
		}
	}
	for _, path := range []string{"/api/v1/snapshots", "/api/v1/verify", "/api/v1/restore", "/api/v1/mobile/files"} {
		request := httptest.NewRequest("POST", path, strings.NewReader("{}"))
		request.Header.Set("Authorization", "Bearer "+viewTestToken)
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		h.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusUnauthorized {
			t.Errorf("viewer accepted privileged operation %s: %d", path, recorder.Code)
		}
	}
	request := httptest.NewRequest("GET", "/api/v1/remotes/about?remote=cloud%3A", nil)
	request.Header.Set("Authorization", "Bearer "+viewTestToken)
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("viewer allowed billable provider call: %d", recorder.Code)
	}
}

func TestViewerInfoRedactsPrivilegedCapabilitiesAndRoot(t *testing.T) {
	h, err := NewHandler(Config{Token: testToken, ViewToken: viewTestToken, SourceRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		token  string
		viewer bool
	}{
		{viewTestToken, true}, {testToken, false},
	} {
		request := httptest.NewRequest("GET", "/api/v1/info", nil)
		request.Header.Set("Authorization", "Bearer "+tc.token)
		recorder := httptest.NewRecorder()
		h.ServeHTTP(recorder, request)
		if recorder.Code != 200 {
			t.Fatalf("get info: %d", recorder.Code)
		}
		var payload map[string]any
		if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if payload["readOnly"] != tc.viewer {
			t.Errorf("incorrect viewer role for %t: %v", tc.viewer, payload)
		}
		for _, name := range []string{"canVerify", "canSnapshot", "canInspectRemote"} {
			if payload[name] != !tc.viewer {
				t.Errorf("wrong %s value: %v", name, payload[name])
			}
		}
		if tc.viewer {
			if _, ok := payload["sourceRoot"]; ok {
				t.Fatal("viewer unexpectedly sees source root")
			}
			for _, key := range []string{"restoreEnabled", "mobileUploadEnabled"} {
				if payload[key] != false {
					t.Errorf("viewer enabled privileged %s", key)
				}
			}
		} else {
			if _, ok := payload["sourceRoot"]; !ok {
				t.Fatal("admin source root missing")
			}
		}
	}
}

func TestViewerAuthorizationRejectsDuplicateHeaders(t *testing.T) {
	h, err := NewHandler(Config{Token: testToken, ViewToken: viewTestToken, SourceRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	for _, values := range [][]string{
		{"Bearer " + viewTestToken, "Bearer " + viewTestToken},
		{"Bearer " + viewTestToken, "Bearer " + testToken},
		{"Bearer " + viewTestToken + ", Bearer " + testToken},
	} {
		request := httptest.NewRequest("GET", "/api/v1/info", nil)
		for _, value := range values {
			request.Header.Add("Authorization", value)
		}
		recorder := httptest.NewRecorder()
		h.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusUnauthorized {
			t.Errorf("accepted ambiguous Authorization header %v", len(values))
		}
	}
}

func TestInvalidViewTokenConfigurationFailsClosed(t *testing.T) {
	source := t.TempDir()
	for _, candidate := range []string{"short", testToken} {
		if _, err := NewHandler(Config{Token: testToken, ViewToken: candidate, SourceRoot: source}); err == nil {
			t.Errorf("accepted unsafe viewer credential")
		}
		t.Setenv("GCB_API_TOKEN", testToken)
		t.Setenv("GCB_VIEW_TOKEN", candidate)
		t.Setenv("GCB_SOURCE_ROOT", source)
		t.Setenv("GCB_LISTEN", "127.0.0.1:8787")
		if _, err := ConfigFromEnv(); err == nil {
			t.Error("environment accepted unsafe viewer credential")
		}
	}
}

func TestUpstreamCommandsNeverReceiveBearerTokens(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX environment regression")
	}
	executable, err := exec.LookPath("env")
	if err != nil {
		t.Skip("env utility unavailable")
	}
	t.Setenv("GCB_API_TOKEN", "synthetic-admin-secret-never-leak")
	t.Setenv("GCB_VIEW_TOKEN", "synthetic-view-secret-never-leak")
	t.Setenv("GCB_SOURCE_ROOT", "synthetic-safe-source")
	result, err := run(context.Background(), executable)
	if err != nil {
		t.Fatal(err)
	}
	text := string(result)
	if strings.Contains(text, "GCB_API_TOKEN=") || strings.Contains(text, "GCB_VIEW_TOKEN=") ||
		strings.Contains(text, "synthetic-admin-secret-never-leak") || strings.Contains(text, "synthetic-view-secret-never-leak") {
		t.Fatal("a bearer credential leaked into the upstream subprocess environment")
	}
	if !strings.Contains(text, "GCB_SOURCE_ROOT=synthetic-safe-source") {
		t.Fatal("noncredential runtime configuration unexpectedly missing")
	}
	if os.Getenv("GCB_API_TOKEN") == "" {
		t.Fatal("test unexpectedly cleared its parent environment")
	}
}

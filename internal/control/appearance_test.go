package control

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// The visual preference is an experimental UI-only capability. Browser tests
// additionally exercise both themes, locked-screen access, and persistence.
func TestLightFirstAppearanceContract(t *testing.T) {
	page, err := os.ReadFile("static/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(page)
	for _, required := range []string{
		`data-gc-mode="light"`,
		`data-gc-accent="iris"`,
		`href="/gc-theme.css"`,
		`id="appearance-toggle"`,
		`id="auth-appearance-toggle"`,
		`aria-pressed="false"`,
	} {
		if !strings.Contains(html, required) {
			t.Errorf("missing appearance UI contract %q", required)
		}
	}

	script, err := os.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(script)
	if !strings.Contains(js, "const preferenceKey = 'gc-backup-appearance'") ||
		!strings.Contains(js, "localStorage.setItem(preferenceKey, mode)") ||
		!strings.Contains(js, "for (const button of appearanceButtons)") {
		t.Fatal("appearance mode is not stored and synchronized as intended")
	}
	if strings.Count(js, "localStorage.setItem(") != 1 {
		t.Fatal("only the non-sensitive UI preference may be persisted by the dashboard")
	}

	theme, err := os.ReadFile("static/gc-theme.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(theme)
	for _, required := range []string{
		`:root[data-gc-mode="light"]`,
		`:root[data-gc-mode="dark"]`,
		`--gc-v1-color-canvas:#ECF0F8;`,
		`--gc-v1-color-primary:#5145D9;`,
		`@media (forced-colors:active)`,
		`.appearance-toggle {min-height:44px;}`,
	} {
		if !strings.Contains(css, required) {
			t.Errorf("missing GC UI semantic color or contrast contract %q", required)
		}
	}
}

func TestAppearanceStylesheetUsesLocalStaticRoute(t *testing.T) {
	server := NewServer(strings.Repeat("x", 32), nil)
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8787/gc-theme.css", nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("theme stylesheet request returned %d", response.Code)
	}
	if got := response.Header().Get("Content-Type"); got != "text/css; charset=utf-8" {
		t.Fatalf("unexpected theme stylesheet content type %q", got)
	}
	if response.Header().Get("Cache-Control") != "no-store" ||
		response.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("theme stylesheet must inherit security headers")
	}
	if !strings.Contains(response.Body.String(), "--gc-v1-color-canvas:#ECF0F8;") {
		t.Fatal("theme stylesheet missing canonical light semantic token")
	}
}

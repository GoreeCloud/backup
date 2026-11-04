package control

import (
	"os"
	"strings"
	"testing"
)

// This is a source contract. Actual mobile navigation layout is separately
// checked in Chromium and Firefox at 320, 390, and 760 CSS pixels.
func TestPhoneNavigationRetainsVisibleSectionLabels(t *testing.T) {
	page, err := os.ReadFile("static/index.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"Overview</a>", "Operations</a>", "Job history</a>"} {
		if !strings.Contains(string(page), label) {
			t.Errorf("missing named section link %q", label)
		}
	}
	css, err := os.ReadFile("static/app.css")
	if err != nil {
		t.Fatal(err)
	}
	const marker = "/* GC UI responsive navigation acceptance:"
	at := strings.LastIndex(string(css), marker)
	if at < 0 {
		t.Fatal("missing explicit phone navigation recovery after compact rail rules")
	}
	phone := string(css[at:])
	for _, requirement := range []string{
		"@media (max-width:760px)",
		".rail nav { display:grid;",
		".brand>span:last-child { display:block;",
		"font-size:12px;",
		"min-height:44px;",
	} {
		if !strings.Contains(phone, requirement) {
			t.Errorf("phone navigation is missing %q", requirement)
		}
	}
}

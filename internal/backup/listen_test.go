package backup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListenNetworkExposureRejected(t *testing.T) {
	allowed := []string{"127.0.0.1:8787", "[::1]:8787", "127.0.0.1:1", "[::1]:65535"}
	for _, value := range allowed {
		if err := validateListen(value); err != nil {
			t.Errorf("%q should be allowed: %v", value, err)
		}
	}
	blocked := []string{"", "0.0.0.0:8787", ":8787", "[::]:8787", "localhost:8787", "my-lan:8787", "192.168.1.1:8787", "[::ffff:127.0.0.1]:8787", "127.0.0.1:0", "127.0.0.1:65536", "127.0.0.1:-1", "127.0.0.1:bad", "127.0.0.1", "127.0.0.1:80:90"}
	for _, value := range blocked {
		if err := validateListen(value); err == nil {
			t.Errorf("%q should have been rejected", value)
		}
	}
}

func TestServerConfigCannotEnableNetworkListener(t *testing.T) {
	for _, address := range []string{"0.0.0.0:8787", "localhost:8787", "[::]:8787"} {
		_, err := NewHandler(Config{Listen: address, Token: testToken, SourceRoot: t.TempDir()})
		if err == nil {
			t.Errorf("NewHandler accepted %q", address)
		}
	}
}

func TestConfigFromEnvRefusesExternalListen(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "files"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GCB_SOURCE_ROOT", root)
	t.Setenv("GCB_API_TOKEN", testToken)
	t.Setenv("GCB_LISTEN", "0.0.0.0:8787")
	if _, err := ConfigFromEnv(); err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("unrestricted listener allowed: %v", err)
	}
	t.Setenv("GCB_LISTEN", "127.0.0.1:8787")
	if _, err := ConfigFromEnv(); err != nil {
		t.Fatal(err)
	}
}

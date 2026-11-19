package backup

import (
	"strings"
	"testing"
)

func TestListenAddressAllowsNumericLoopbackOnly(t *testing.T) {
	for _, address := range []string{"127.0.0.1:8787", "[::1]:8787", "127.0.0.1:65535"} {
		if err := validateLocalListen(address); err != nil {
			t.Errorf("valid %s rejected: %v", address, err)
		}
	}
	for _, address := range []string{
		"", ":8787", "0.0.0.0:8787", "[::]:8787", "localhost:8787",
		"192.168.1.2:8787", "127.0.0.2:8787", "[::ffff:127.0.0.1]:8787",
		"127.0.0.1:0", "127.0.0.1:-1", "127.0.0.1:65536",
		"127.0.0.1:abc", "127.0.0.1", "https://127.0.0.1:8787",
		"127.0.0.1:8787/other",
	} {
		if err := validateLocalListen(address); err == nil {
			t.Errorf("unsafe %s accepted", address)
		}
	}
}

func TestStartupRejectsPublicBindAddress(t *testing.T) {
	for _, listen := range []string{"0.0.0.0:8787", "[::]:8787", "10.0.0.10:8787"} {
		t.Run(listen, func(t *testing.T) {
			t.Setenv("GCB_API_TOKEN", strings.Repeat("x", 48))
			t.Setenv("GCB_SOURCE_ROOT", t.TempDir())
			t.Setenv("GCB_LISTEN", listen)
			if _, err := ConfigFromEnv(); err == nil {
				t.Fatal("public endpoint accepted by environment parser")
			}
			if _, err := NewHandler(Config{Token: strings.Repeat("x", 48), Listen: listen, SourceRoot: t.TempDir()}); err == nil {
				t.Fatal("public endpoint accepted by handler factory")
			}
		})
	}
}
func TestLocalConfigStartup(t *testing.T) {
	t.Setenv("GCB_API_TOKEN", strings.Repeat("x", 48))
	t.Setenv("GCB_SOURCE_ROOT", t.TempDir())
	t.Setenv("GCB_LISTEN", "127.0.0.1:7001")
	cfg, err := ConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != "127.0.0.1:7001" {
		t.Fatal("unexpected listen address")
	}
	if _, err := NewHandler(cfg); err != nil {
		t.Fatal(err)
	}
}

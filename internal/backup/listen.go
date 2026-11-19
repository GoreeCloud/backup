package backup

import (
	"errors"
	"net"
	"strconv"
)

// Only loopback HTTP is offered by the API. Remote devices must use an
// independent authenticated TLS reverse proxy or protected tunnel.
func validateLocalListen(listen string) error {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return errors.New("GCB_LISTEN must be an explicit loopback IP and port")
	}
	if host != "127.0.0.1" && host != "::1" {
		return errors.New("GCB_LISTEN must bind only to 127.0.0.1 or [::1]; use an external TLS proxy for remote access")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return errors.New("GCB_LISTEN port must be between 1 and 65535")
	}
	return nil
}

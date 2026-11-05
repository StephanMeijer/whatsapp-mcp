// SPDX-License-Identifier: MPL-2.0

package main

import (
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/StephanMeijer/whatsapp-mcp/internal/config"
)

// healthcheckTimeout bounds one probe, well inside the image's HEALTHCHECK timeout.
const healthcheckTimeout = 3 * time.Second

// healthcheck asks the server listening on addr whether it is alive, for the
// container health check: the image is distroless, so there is no curl or
// shell to do it. An empty addr is the default listen address.
func healthcheck(addr string) error {
	url, err := healthURL(addr)
	if err != nil {
		return err
	}
	// No proxy: one set for the server's traffic to WhatsApp must not carry
	// a request to the server itself when it listens on a non-loopback address.
	client := &http.Client{
		Timeout:   healthcheckTimeout,
		Transport: &http.Transport{Proxy: nil},
	}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
	}
	return nil
}

// healthURL is the /healthz URL of a server listening on addr. A server
// listening on every interface is reached on loopback.
func healthURL(addr string) (string, error) {
	if addr == "" {
		addr = config.DefaultAddr
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("listen address %q: %w", addr, err)
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/healthz", nil
}

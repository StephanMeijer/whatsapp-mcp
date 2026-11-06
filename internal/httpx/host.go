// SPDX-License-Identifier: MPL-2.0

package httpx

import (
	"net"
	"net/http"
	"strings"
)

// AllowHosts refuses requests whose Host is neither one of hosts nor a
// loopback name, so that a DNS name pointed at this server cannot be used
// to reach it from a browser (DNS rebinding). It stands in for the MCP SDK's
// own check, which accepts only loopback hosts on a loopback listener and so
// refuses a reverse proxy that passes on the public Host. Ports are ignored;
// hosts are compared without regard to case.
func AllowHosts(hosts []string, next http.Handler) http.Handler {
	allowed := make(map[string]bool, len(hosts))
	for _, h := range hosts {
		allowed[strings.ToLower(hostname(h))] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := strings.ToLower(hostname(r.Host))
		if !allowed[host] && !IsLoopback(host) {
			http.Error(w, "forbidden: unexpected Host header", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// hostname strips the port, and the brackets of an IPv6 literal, from a Host.
func hostname(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return h
	}
	return strings.TrimSuffix(strings.TrimPrefix(hostport, "["), "]")
}

// IsLoopback reports whether host is localhost or a loopback IP; ports already stripped.
func IsLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

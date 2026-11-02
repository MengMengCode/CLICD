package server

import (
	"net"
	"strconv"
	"strings"
)

// serverListenAddress accepts IPv4, IPv6, and host names from CLICD_BIND_ADDR.
func serverListenAddress(host string, port int) string {
	host = strings.TrimSpace(host)
	if host == "" {
		host = "0.0.0.0"
	}
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = host[1 : len(host)-1]
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

package security

import "net"

// IsLoopbackHost reports whether host is "localhost" or a loopback IP
// literal. It never resolves a name, so a DNS answer can't make a remote
// host count as local.
func IsLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

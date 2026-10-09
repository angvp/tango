package security

import (
	"net"
	"net/http"
	"testing"
)

func proxies(t *testing.T, cidrs ...string) []*net.IPNet {
	t.Helper()
	var out []*net.IPNet
	for _, cidr := range cidrs {
		_, n, err := net.ParseCIDR(cidr)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, n)
	}
	return out
}

func request(peer string, forwardedFor ...string) *http.Request {
	r := &http.Request{RemoteAddr: peer, Header: http.Header{}}
	for _, v := range forwardedFor {
		r.Header.Add("X-Forwarded-For", v)
	}
	return r
}

func TestClientKeyWithoutProxiesIsTheRemoteHostAndReadsNoHeader(t *testing.T) {
	if got := ClientKey(nil)(request("192.0.2.1:5555", "203.0.113.9")); got != "192.0.2.1" {
		t.Fatalf("key = %q, want the remote host 192.0.2.1", got)
	}
}

func TestClientKeyFallsBackToTheRawRemoteAddr(t *testing.T) {
	if got := ClientKey(nil)(request("no-port-here")); got != "no-port-here" {
		t.Fatalf("key = %q, want the raw RemoteAddr", got)
	}
}

func TestClientKeyIgnoresForwardedForFromAnUntrustedPeer(t *testing.T) {
	key := ClientKey(proxies(t, "10.0.0.0/8"))
	if got := key(request("198.51.100.1:80", "203.0.113.9")); got != "198.51.100.1" {
		t.Fatalf("key = %q, want the untrusted peer's own address", got)
	}
}

// A proxy that appends (nginx's default) forwards "<what the client sent>,
// <the client's real address>". The client's own part must not choose the key.
func TestClientKeyUsesTheAddressTheTrustedProxySaw(t *testing.T) {
	key := ClientKey(proxies(t, "10.0.0.0/8"))
	for _, claimed := range []string{"1.1.1.1", "2.2.2.2", "9.9.9.9, 8.8.8.8"} {
		if got := key(request("10.0.0.5:80", claimed+", 203.0.113.7")); got != "203.0.113.7" {
			t.Fatalf("claimed %q: key = %q, want 203.0.113.7 whatever the client wrote first", claimed, got)
		}
	}
}

func TestClientKeySkipsTrustedHopsFromTheRight(t *testing.T) {
	key := ClientKey(proxies(t, "10.0.0.0/8"))
	if got := key(request("10.0.0.5:80", "203.0.113.7, 10.0.0.9")); got != "203.0.113.7" {
		t.Fatalf("key = %q, want the first untrusted address from the right", got)
	}
	// Several header lines count as one list, in order.
	if got := key(request("10.0.0.5:80", "1.1.1.1", "203.0.113.7, 10.0.0.9")); got != "203.0.113.7" {
		t.Fatalf("two header lines: key = %q, want 203.0.113.7", got)
	}
}

func TestClientKeyFallsBackToThePeerWhenTheHeaderIsNotUsable(t *testing.T) {
	key := ClientKey(proxies(t, "10.0.0.0/8"))
	for name, r := range map[string]*http.Request{
		"no header":         request("10.0.0.5:80"),
		"only trusted hops": request("10.0.0.5:80", "10.0.0.7, 10.0.0.9"),
		"garbage":           request("10.0.0.5:80", "not-an-ip"),
		"garbage on right":  request("10.0.0.5:80", "203.0.113.7, junk"),
		"empty entry":       request("10.0.0.5:80", ", 203.0.113.7,"),
	} {
		if got := key(r); got != "10.0.0.5" {
			t.Fatalf("%s: key = %q, want the proxy's own address 10.0.0.5", name, got)
		}
	}
}

func TestClientKeyReadsNoOtherHeader(t *testing.T) {
	r := request("10.0.0.5:80")
	r.Header.Set("X-Real-IP", "203.0.113.7")
	if got := ClientKey(proxies(t, "10.0.0.0/8"))(r); got != "10.0.0.5" {
		t.Fatalf("key = %q, want X-Real-IP ignored", got)
	}
}

// One client must be one bucket however the address is written.
func TestClientKeyCanonicalisesTheForwardedAddress(t *testing.T) {
	key := ClientKey(proxies(t, "10.0.0.0/8"))
	same := [][]string{
		{"2001:DB8::1", "2001:db8:0:0:0:0:0:1", "2001:db8::1"},
		{"203.0.113.7", "::ffff:203.0.113.7"},
	}
	for _, group := range same {
		want := key(request("10.0.0.5:80", group[0]))
		for _, spelling := range group[1:] {
			if got := key(request("10.0.0.5:80", spelling)); got != want {
				t.Fatalf("%q gave key %q, want %q (same client)", spelling, got, want)
			}
		}
	}
	if got := key(request("10.0.0.5:80", "203.0.113.7:4455")); got != "10.0.0.5" {
		t.Fatalf("an address with a port is not a valid hop; key = %q", got)
	}
}

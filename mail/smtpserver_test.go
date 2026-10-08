package mail_test

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"math/big"
	"net"
	"net/textproto"
	"strings"
	"sync"
	"testing"
	"time"
)

// smtpServer is an in-process SMTP server on a loopback port, recording
// what clients send it.
type smtpServer struct {
	addr string
	// startTLS offers STARTTLS; implicitTLS speaks TLS from the first byte;
	// stall accepts connections and never answers.
	startTLS, implicitTLS, stall bool
	// failOn names a command the server refuses with a reply echoing
	// everything the client told it.
	failOn    string
	tlsConfig *tls.Config

	mu       sync.Mutex
	auth     []string // decoded AUTH PLAIN credentials
	from     []string
	to       []string
	data     []string
	sawTLS   []bool // whether each MAIL arrived over TLS
	commands []string
}

// testCert returns a self-signed certificate for 127.0.0.1 and a pool
// trusting it.
func testCert(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, pool
}

// startSMTPServer starts s and returns the pool trusting its certificate.
func startSMTPServer(t *testing.T, s *smtpServer) *x509.CertPool {
	t.Helper()
	cert, pool := testCert(t)
	s.tlsConfig = &tls.Config{Certificates: []tls.Certificate{cert}}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	s.addr = listener.Addr().String()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go s.serve(conn)
		}
	}()
	return pool
}

func (s *smtpServer) serve(conn net.Conn) {
	defer conn.Close()
	if s.stall {
		_, _ = bufio.NewReader(conn).ReadString('\n')
		time.Sleep(time.Minute)
		return
	}
	secure := false
	if s.implicitTLS {
		conn = tls.Server(conn, s.tlsConfig)
		secure = true
	}
	text := textproto.NewConn(conn)
	reply := func(lines ...string) { _ = text.PrintfLine("%s", strings.Join(lines, "\r\n")) }
	reply("220 test ESMTP")
	for {
		line, err := text.ReadLine()
		if err != nil {
			return
		}
		verb := strings.ToUpper(strings.Fields(line + " x")[0])
		s.mu.Lock()
		s.commands = append(s.commands, verb)
		s.mu.Unlock()
		if verb == s.failOn {
			if verb == "DATA" {
				reply("354 go")
				data, _ := text.ReadDotBytes()
				reply("554 rejected: " + strings.ReplaceAll(string(data), "\r\n", " "))
				continue
			}
			decoded := ""
			if fields := strings.Fields(line); len(fields) > 2 {
				raw, _ := base64.StdEncoding.DecodeString(fields[2])
				decoded = strings.ReplaceAll(string(raw), "\x00", " ")
			}
			reply("535 refused " + line + " " + decoded)
			continue
		}
		switch verb {
		case "EHLO", "HELO":
			lines := []string{"250-test"}
			if s.startTLS && !secure {
				lines = append(lines, "250-STARTTLS")
			}
			lines = append(lines, "250 AUTH PLAIN")
			reply(lines...)
		case "STARTTLS":
			reply("220 go ahead")
			tlsConn := tls.Server(conn, s.tlsConfig)
			if err := tlsConn.Handshake(); err != nil {
				return
			}
			conn, secure = tlsConn, true
			text = textproto.NewConn(conn)
		case "AUTH":
			fields := strings.Fields(line)
			decoded, _ := base64.StdEncoding.DecodeString(fields[len(fields)-1])
			s.mu.Lock()
			s.auth = append(s.auth, string(decoded))
			s.mu.Unlock()
			reply("235 ok")
		case "MAIL":
			s.mu.Lock()
			s.from = append(s.from, line)
			s.sawTLS = append(s.sawTLS, secure)
			s.mu.Unlock()
			reply("250 ok")
		case "RCPT":
			s.mu.Lock()
			s.to = append(s.to, line)
			s.mu.Unlock()
			reply("250 ok")
		case "DATA":
			reply("354 go")
			data, err := text.ReadDotBytes()
			if err != nil {
				return
			}
			s.mu.Lock()
			s.data = append(s.data, string(data))
			s.mu.Unlock()
			reply("250 queued")
		case "QUIT":
			reply("221 bye")
			return
		default:
			reply("250 ok")
		}
	}
}

// smtpRecord is what an smtpServer has received so far.
type smtpRecord struct {
	auth, from, to, data, commands []string
	sawTLS                         []bool
}

func (s *smtpServer) snapshot() smtpRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	return smtpRecord{auth: s.auth, from: s.from, to: s.to, data: s.data, sawTLS: s.sawTLS, commands: s.commands}
}

package mail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"net/url"
	"os"
	"time"
)

// SMTPURLEnv is the environment variable SMTPSenderFromEnv reads.
const SMTPURLEnv = "TANGO_SMTP_URL"

// DefaultSMTPTimeout bounds one SMTP exchange when Send's context has no
// deadline.
const DefaultSMTPTimeout = 30 * time.Second

// ErrNotConfigured is returned by SMTPSenderFromEnv when TANGO_SMTP_URL is
// unset or empty.
var ErrNotConfigured = errors.New("mail: TANGO_SMTP_URL is not set")

// smtpMode is how an SMTPSender secures its connection.
type smtpMode int

const (
	modeSTARTTLS smtpMode = iota // smtp://: STARTTLS, required
	modeTLS                      // smtps://: TLS from the first byte
	modePlain                    // smtp+insecure://: plaintext, loopback only
)

// SMTPSender delivers mail through an SMTP server, opening one connection
// per message. Create one with NewSMTPSender or SMTPSenderFromEnv. It is
// safe for concurrent use.
type SMTPSender struct {
	mode               smtpMode
	host, addr         string
	username, password string
	options            options
}

// NewSMTPSender returns a sender for the server rawURL names. The scheme
// chooses how the connection is secured:
//
//	smtp://user:pass@host:587          STARTTLS, required: a server that doesn't offer it is refused
//	smtps://user:pass@host:465         TLS from the first byte
//	smtp+insecure://localhost:1025     plaintext, for a local tool such as Mailpit
//
// smtp+insecure is accepted only for a loopback host (localhost or a
// loopback IP) and never with credentials. The port defaults to 587, 465
// and 25 respectively. Credentials, if any, are sent with AUTH PLAIN.
// Errors never contain the URL, so a password can't leak into logs.
func NewSMTPSender(rawURL string, opts ...Option) (*SMTPSender, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, errors.New("mail: the SMTP URL is malformed")
	}
	s := &SMTPSender{options: newOptions(opts)}
	var defaultPort string
	switch u.Scheme {
	case "smtp":
		s.mode, defaultPort = modeSTARTTLS, "587"
	case "smtps":
		s.mode, defaultPort = modeTLS, "465"
	case "smtp+insecure":
		s.mode, defaultPort = modePlain, "25"
	default:
		return nil, errors.New("mail: the SMTP URL's scheme must be smtp, smtps or smtp+insecure")
	}
	s.host = u.Hostname()
	if s.host == "" || u.Opaque != "" {
		return nil, errors.New("mail: the SMTP URL has no host")
	}
	port := u.Port()
	if port == "" {
		port = defaultPort
	}
	s.addr = net.JoinHostPort(s.host, port)
	if u.User != nil {
		s.username = u.User.Username()
		s.password, _ = u.User.Password()
	}
	if s.mode == modePlain {
		if !isLoopback(s.host) {
			return nil, errors.New("mail: smtp+insecure is only allowed to a loopback host")
		}
		if u.User != nil {
			return nil, errors.New("mail: smtp+insecure never sends credentials; use smtp or smtps")
		}
	}
	return s, nil
}

// SMTPSenderFromEnv returns NewSMTPSender for TANGO_SMTP_URL, or
// ErrNotConfigured when it is unset or empty.
func SMTPSenderFromEnv(opts ...Option) (*SMTPSender, error) {
	rawURL := os.Getenv(SMTPURLEnv)
	if rawURL == "" {
		return nil, ErrNotConfigured
	}
	return NewSMTPSender(rawURL, opts...)
}

// isLoopback reports whether host is localhost or a loopback IP, without
// resolving it.
func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Send validates message, then delivers it in one SMTP exchange bounded by
// ctx's deadline, or DefaultSMTPTimeout when ctx has none.
func (s *SMTPSender) Send(ctx context.Context, message Message) error {
	encoded, err := encode(message, s.options)
	if err != nil {
		return err
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.options.defaultTimeout)
		defer cancel()
	}
	if err := s.deliver(ctx, encoded); err != nil {
		return fmt.Errorf("mail: sending through %s: %w", s.addr, err)
	}
	return nil
}

func (s *SMTPSender) deliver(ctx context.Context, message encoded) error {
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", s.addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline)
	// Cancelling ctx ends a blocked read or write at once.
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Unix(1, 0)) })
	defer stop()

	tlsConfig := &tls.Config{ServerName: s.host, RootCAs: s.options.rootCAs, MinVersion: tls.VersionTLS12}
	if s.mode == modeTLS {
		tlsConn := tls.Client(conn, tlsConfig)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			return err
		}
		conn = tlsConn
	}
	client, err := smtp.NewClient(conn, s.host)
	if err != nil {
		return err
	}
	defer client.Close()
	if s.mode == modeSTARTTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errors.New("the server doesn't offer STARTTLS")
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			return err
		}
	}
	if s.username != "" {
		if err := client.Auth(smtp.PlainAuth("", s.username, s.password, s.host)); err != nil {
			return err
		}
	}
	if err := client.Mail(message.from); err != nil {
		return err
	}
	if err := client.Rcpt(message.to); err != nil {
		return err
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(message.raw); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return client.Quit()
}

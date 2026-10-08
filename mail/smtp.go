package mail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"net/textproto"
	"net/url"
	"os"
	"time"

	"github.com/angvp/tango/internal/security"
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
// loopback IP), never with credentials, and only with an explicit port, so
// plaintext is always a deliberate choice. smtp and smtps default to ports
// 587 and 465. Credentials, if any, are sent with AUTH PLAIN. Errors never
// contain the URL, so a password can't leak into logs.
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
		s.mode = modePlain
	default:
		return nil, errors.New("mail: the SMTP URL's scheme must be smtp, smtps or smtp+insecure")
	}
	s.host = u.Hostname()
	if s.host == "" || u.Opaque != "" {
		return nil, errors.New("mail: the SMTP URL has no host")
	}
	port := u.Port()
	if port == "" {
		if s.mode == modePlain {
			return nil, errors.New("mail: smtp+insecure needs an explicit port, such as smtp+insecure://localhost:1025")
		}
		port = defaultPort
	}
	s.addr = net.JoinHostPort(s.host, port)
	if u.User != nil {
		s.username = u.User.Username()
		s.password, _ = u.User.Password() // an absent password is an empty one
	}
	if s.mode == modePlain {
		if !security.IsLoopbackHost(s.host) {
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

// Send validates message, then delivers it in one SMTP exchange bounded by
// ctx's deadline, or DefaultSMTPTimeout when ctx has none. A server's
// refusal is reported by stage and reply code only: its text can echo
// whatever the client sent.
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
	if stage, err := s.deliver(ctx, encoded); err != nil {
		return s.sendError(stage, err)
	}
	return nil
}

// deliver runs one SMTP exchange, returning the stage that failed.
func (s *SMTPSender) deliver(ctx context.Context, message encoded) (string, error) {
	client, closeConn, err := s.connect(ctx)
	if err != nil {
		return "connecting", err
	}
	defer closeConn()
	defer client.Close()
	if s.username != "" {
		if err := client.Auth(smtp.PlainAuth("", s.username, s.password, s.host)); err != nil {
			return "authentication", err
		}
	}
	if err := client.Mail(message.from); err != nil {
		return "the sender", err
	}
	if err := client.Rcpt(message.to); err != nil {
		return "the recipient", err
	}
	w, err := client.Data()
	if err != nil {
		return "the message", err
	}
	if _, err := w.Write(message.raw); err != nil {
		return "the message", err
	}
	if err := w.Close(); err != nil {
		return "the message", err
	}
	return "closing", client.Quit()
}

// connect dials the server and secures the connection as the URL's
// scheme says, bounding everything by ctx. closeConn releases what it set
// up.
func (s *SMTPSender) connect(ctx context.Context) (client *smtp.Client, closeConn func(), err error) {
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", s.addr)
	if err != nil {
		return nil, nil, err
	}
	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline) // a net.Conn from Dial accepts a deadline
	// Cancelling ctx ends a blocked read or write at once.
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Unix(1, 0)) })
	closeConn = func() { stop(); _ = conn.Close() }

	tlsConfig := &tls.Config{ServerName: s.host, RootCAs: s.options.rootCAs, MinVersion: tls.VersionTLS12}
	if s.mode == modeTLS {
		tlsConn := tls.Client(conn, tlsConfig)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			closeConn()
			return nil, nil, err
		}
		conn = tlsConn
	}
	client, err = smtp.NewClient(conn, s.host)
	if err != nil {
		closeConn()
		return nil, nil, err
	}
	if s.mode == modeSTARTTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			closeConn()
			return nil, nil, errors.New("the server doesn't offer STARTTLS")
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			closeConn()
			return nil, nil, err
		}
	}
	return client, closeConn, nil
}

// sendError reports a failed exchange without anything secret in it: a
// server's refusal keeps only its stage and reply code, and any other
// failure only its stage. Neither carries the original text, which can
// quote what tanGO sent, credentials included.
func (s *SMTPSender) sendError(stage string, err error) error {
	var reply *textproto.Error
	if errors.As(err, &reply) {
		return &smtpError{text: fmt.Sprintf("mail: the SMTP server at %s refused %s (code %d)", s.addr, stage, reply.Code), cause: err}
	}
	return &smtpError{text: fmt.Sprintf("mail: sending through %s failed at %s", s.addr, stage), cause: err}
}

// smtpError is a delivery failure whose text is safe to log. It doesn't
// unwrap, so the original text can't be recovered from it, but errors.Is
// still matches its cause, such as context.DeadlineExceeded.
type smtpError struct {
	text  string
	cause error
}

func (e *smtpError) Error() string { return e.text }

func (e *smtpError) Is(target error) bool { return errors.Is(e.cause, target) }

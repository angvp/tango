package accounts

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/mail"
	"net/url"
	"strings"

	tangomail "github.com/angvp/tango/mail"
)

// EventMailFailed is logged when the outbox can't send an email: the
// Sender failed, or preparing the email did. Its attributes are purpose
// and a redacted error; never the address, the link or the message.
const EventMailFailed = "tango.accounts.mail_failed"

// EventMailDropped is logged when the outbox is full and an email is
// dropped. Its only attribute is purpose. The HTTP response is unchanged.
const EventMailDropped = "tango.accounts.mail_dropped"

// MailConfig turns on password reset and email verification.
type MailConfig struct {
	// Sender delivers the emails, for example a *mail.SMTPSender.
	Sender tangomail.Sender
	// From is the address the emails come from, such as
	// "Shop <noreply@example.com>".
	From string
	// BaseURL is the Public base URL links are built on: an absolute
	// https:// origin such as "https://example.com", or http:// for
	// localhost or a loopback IP during development. It has no path. Links
	// are never built from the request's Host.
	BaseURL string
	// Logger receives EventMailFailed and EventMailDropped. Nil uses
	// slog.Default.
	Logger *slog.Logger
}

// WithMail turns on password reset and email verification, sending through
// config.Sender. Without it, their routes aren't mounted and accounts sends
// no email. config is validated when the app registers, so RunRegistration
// (and -check) fail on a missing Sender, a malformed From, or a BaseURL
// that isn't an absolute https:// origin. See ADR 0045.
func WithMail(config MailConfig) Option {
	return func(c *accountsConfig) { c.mail = &config }
}

// baseURL returns config's BaseURL without a trailing slash, or the reason
// config can't be used.
func (config MailConfig) validate() (string, error) {
	if config.Sender == nil {
		return "", errors.New("accounts: MailConfig.Sender is nil")
	}
	if strings.ContainsAny(config.From, "\r\n") {
		return "", errors.New("accounts: MailConfig.From contains a line break")
	}
	if _, err := mail.ParseAddress(config.From); err != nil {
		return "", errors.New("accounts: MailConfig.From is not one email address")
	}
	u, err := url.Parse(config.BaseURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", fmt.Errorf("accounts: MailConfig.BaseURL must be an absolute origin such as https://example.com, got %q", config.BaseURL)
	}
	switch {
	case u.Scheme == "https":
	case u.Scheme == "http" && isLoopbackHost(u.Hostname()):
	default:
		return "", fmt.Errorf("accounts: MailConfig.BaseURL must use https (http only for localhost), got %q", config.BaseURL)
	}
	return u.Scheme + "://" + u.Host, nil
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

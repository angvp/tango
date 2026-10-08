package accounts

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/internal/security"
	tangomail "github.com/angvp/tango/mail"
)

// EventMailFailed is logged when the outbox can't send an email: the
// Sender failed, or preparing the email did. Its attributes are purpose
// and a redacted error; never the address, the link or the message.
const EventMailFailed = "tango.accounts.mail_failed"

// EventMailDropped is logged when the outbox is full and an email is
// dropped. Its only attribute is purpose. The HTTP response is unchanged.
const EventMailDropped = "tango.accounts.mail_dropped"

// Mail flows' limits: how long each kind of link lasts, the per-address
// cooldown between emails, and how many requests one client IP may make.
const (
	resetTokenLifetime        = time.Hour
	verificationTokenLifetime = 24 * time.Hour
	mailCooldown              = 5 * time.Minute
	mailRateLimitAttempts     = 5
	mailRateLimitWindow       = time.Minute
)

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

// validate returns config's BaseURL as an origin without a trailing slash,
// or the reason config can't be used.
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
	if u.Scheme != "https" && (u.Scheme != "http" || !security.IsLoopbackHost(u.Hostname())) {
		return "", fmt.Errorf("accounts: MailConfig.BaseURL must use https (http only for localhost), got %q", config.BaseURL)
	}
	return u.Scheme + "://" + u.Host, nil
}

// mailer is what the mail flows share: the store, accounts' configuration,
// the validated base URL, the outbox and the per-address cooldown.
type mailer struct {
	store    *db.Store
	cfg      accountsConfig
	baseURL  string
	outbox   *outbox
	cooldown *cooldown
}

// newMailer validates cfg.mail and registers the outbox's worker.
func newMailer(registry *tango.Registry, store *db.Store, cfg accountsConfig) (*mailer, error) {
	baseURL, err := cfg.mail.validate()
	if err != nil {
		return nil, err
	}
	m := &mailer{
		store:    store,
		cfg:      cfg,
		baseURL:  baseURL,
		outbox:   newOutbox(cfg.outboxCapacity, cfg.mail.Sender, cfg.mail.Logger),
		cooldown: newCooldown(mailCooldown),
	}
	if err := registry.RegisterLifecycle(m.outbox.lifecycle()); err != nil {
		return nil, err
	}
	return m, nil
}

// routes are the mail flows' routes, mounted only with WithMail.
func (m *mailer) routes() tango.URLs {
	reset := passwordResetView(m, security.NewRateLimiter(mailRateLimitAttempts, mailRateLimitWindow))
	confirm := m.tokenLinkView(m.resetLink())
	verify := m.tokenLinkView(m.verifyLink())
	resend := resendVerificationView(m, security.NewRateLimiter(mailRateLimitAttempts, mailRateLimitWindow))
	return tango.URLs{
		tango.Path(http.MethodGet, "/accounts/password-reset/", reset),
		tango.Path(http.MethodPost, "/accounts/password-reset/", reset),
		tango.Path(http.MethodGet, "/accounts/password-reset/confirm/", confirm),
		tango.Path(http.MethodPost, "/accounts/password-reset/confirm/", confirm),
		tango.Path(http.MethodGet, "/accounts/verify/", verify),
		tango.Path(http.MethodPost, "/accounts/verify/", verify),
		tango.Path(http.MethodPost, "/accounts/verify/resend/", resend),
	}
}

// queue queues job for email unless email is in its cooldown for the
// job's purpose. The cooldown is taken only when the job is admitted: a
// job the full outbox drops gives it back, so dropping never locks a
// person out. Unknown addresses take a cooldown too, so it never depends
// on whether an account exists.
func (m *mailer) queue(ctx context.Context, email string, job outboxJob) {
	key := cooldownKey{purpose: job.purpose, email: email}
	at := m.cfg.now()
	if !m.cooldown.reserve(key, at) {
		return
	}
	if !m.outbox.enqueue(ctx, job) {
		m.cooldown.release(key, at)
	}
}

// emailWithLink is the delivery for an email to account carrying token in
// a link to path.
func (m *mailer) emailWithLink(account Account, token, path, subject, text string) delivery {
	link := m.baseURL + path + "?token=" + token
	return delivery{
		message: tangomail.Message{From: m.cfg.mail.From, To: account.Email, Subject: subject, Text: fmt.Sprintf(text, link)},
		secret:  token,
	}
}

// cooldownKey is one address's cooldown for one kind of email.
type cooldownKey struct {
	purpose TokenPurpose
	email   string
}

// cooldown allows one email per key per window, in memory. Expired entries
// are pruned on every call, so it holds at most what the window allows.
type cooldown struct {
	mu     sync.Mutex
	window time.Duration
	last   map[cooldownKey]time.Time
}

func newCooldown(window time.Duration) *cooldown {
	return &cooldown{window: window, last: map[cooldownKey]time.Time{}}
}

// reserve reports whether key may send at now, taking its cooldown if so.
func (c *cooldown) reserve(key cooldownKey, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, at := range c.last {
		if now.Sub(at) >= c.window {
			delete(c.last, k)
		}
	}
	if _, cooling := c.last[key]; cooling {
		return false
	}
	c.last[key] = now
	return true
}

// release gives back the cooldown reserve took at at, unless a later
// reservation replaced it.
func (c *cooldown) release(key cooldownKey, at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if last, ok := c.last[key]; ok && last.Equal(at) {
		delete(c.last, key)
	}
}

package accounts

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/angvp/tango"
	"github.com/angvp/tango/internal/observabilitysafe"
	tangomail "github.com/angvp/tango/mail"
)

// defaultOutboxCapacity is how many emails the outbox holds before it
// drops new ones.
const defaultOutboxCapacity = 100

// delivery is one prepared email and the secret its link carries, which a
// failure log must never show.
type delivery struct {
	message tangomail.Message
	secret  string
}

// outboxJob prepares one email in the background: it looks up whatever it
// needs and returns the email, or ok=false when there is nothing to send
// (an unknown or inactive account, say). Doing that work here, not in the
// request, keeps every request the same however the lookup turns out.
type outboxJob struct {
	purpose TokenPurpose
	prepare func(ctx context.Context) (d delivery, ok bool, err error)
}

// outbox is accounts' bounded in-memory email queue, drained one email at
// a time by one worker that runs as a Lifecycle component. A full queue
// drops the email; a stopped worker first sends what its deadline allows.
// Nothing survives a crash and nothing is retried (ADR 0033).
type outbox struct {
	queue  chan outboxJob
	sender tangomail.Sender
	logger *slog.Logger

	mu   sync.Mutex
	stop chan context.Context
	done chan struct{}
}

func newOutbox(capacity int, sender tangomail.Sender, logger *slog.Logger) *outbox {
	if logger == nil {
		logger = slog.Default()
	}
	return &outbox{queue: make(chan outboxJob, capacity), sender: sender, logger: logger}
}

func (o *outbox) lifecycle() tango.Lifecycle {
	return tango.Lifecycle{Name: "accounts.outbox", Start: o.start, Stop: o.shutdown}
}

// enqueue queues job, or drops it and logs EventMailDropped when the queue
// is full. It never blocks.
func (o *outbox) enqueue(ctx context.Context, job outboxJob) {
	select {
	case o.queue <- job:
	default:
		o.log(ctx, EventMailDropped, slog.String("purpose", string(job.purpose)))
	}
}

func (o *outbox) start(ctx context.Context) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.stop, o.done = make(chan context.Context, 1), make(chan struct{})
	// The application context is cancelled when shutdown begins; the
	// worker keeps going until Stop, which bounds the rest.
	go o.run(context.WithoutCancel(ctx), o.stop, o.done)
	return nil
}

func (o *outbox) run(ctx context.Context, stop <-chan context.Context, done chan<- struct{}) {
	defer close(done)
	for {
		select {
		case job := <-o.queue:
			o.deliver(ctx, job)
		case stopCtx := <-stop:
			for stopCtx.Err() == nil {
				select {
				case job := <-o.queue:
					o.deliver(stopCtx, job)
				default:
					return
				}
			}
			return
		}
	}
}

// shutdown sends what's queued before ctx's deadline, then stops the
// worker. Emails still queued at the deadline are lost.
func (o *outbox) shutdown(ctx context.Context) error {
	o.mu.Lock()
	stop, done := o.stop, o.done
	o.stop, o.done = nil, nil
	o.mu.Unlock()
	if stop == nil {
		return nil
	}
	stop <- ctx
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (o *outbox) deliver(ctx context.Context, job outboxJob) {
	d, ok, err := job.prepare(ctx)
	if err == nil && ok {
		err = o.sender.Send(ctx, d.message)
	}
	if err != nil {
		o.log(ctx, EventMailFailed, slog.String("purpose", string(job.purpose)), slog.String("error", redact(err.Error(), d)))
	}
}

func (o *outbox) log(ctx context.Context, event string, attrs ...slog.Attr) {
	observabilitysafe.Call(func() { o.logger.LogAttrs(ctx, slog.LevelError, event, attrs...) })
}

// redact removes from text the email's recipient, its text (whole and line
// by line) and its secret, so a Sender's error that echoes them can be
// logged.
func redact(text string, d delivery) string {
	secrets := []string{d.message.To, d.message.Text, d.secret}
	secrets = append(secrets, strings.Split(d.message.Text, "\n")...)
	sort.Slice(secrets, func(i, j int) bool { return len(secrets[i]) > len(secrets[j]) })
	for _, s := range secrets {
		if s = strings.TrimSpace(s); s != "" {
			text = replaceFold(text, s, "[redacted]")
		}
	}
	return text
}

// replaceFold replaces every case-insensitive occurrence of old in s.
func replaceFold(s, old, replacement string) string {
	lowerS, lowerOld := strings.ToLower(s), strings.ToLower(old)
	if len(lowerS) != len(s) || len(lowerOld) != len(old) {
		return strings.ReplaceAll(s, old, replacement) // case mapping changed lengths
	}
	var b strings.Builder
	for {
		i := strings.Index(lowerS, lowerOld)
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:i])
		b.WriteString(replacement)
		s, lowerS = s[i+len(old):], lowerS[i+len(old):]
	}
}

// cooldown allows one email per key per window, in memory. Expired entries
// are pruned on every call, so it holds at most what the window allows.
type cooldown struct {
	mu     sync.Mutex
	window time.Duration
	last   map[string]time.Time
}

func newCooldown(window time.Duration) *cooldown {
	return &cooldown{window: window, last: map[string]time.Time{}}
}

// allow reports whether key may send at now, recording it if so.
func (c *cooldown) allow(key string, now time.Time) bool {
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

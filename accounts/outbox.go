package accounts

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"

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
// drops the email. Stop sends what's queued until its deadline, then
// cancels the send in progress and waits for the worker to return, so no
// email is sent after Stop returns. Nothing survives a crash and nothing
// is retried (ADR 0033).
type outbox struct {
	queue  chan outboxJob
	sender tangomail.Sender
	logger *slog.Logger

	mu     sync.Mutex
	stop   chan struct{}      // closed by Stop
	done   chan struct{}      // closed when the worker returns
	cancel context.CancelFunc // ends the worker's sends
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

// enqueue queues job and reports true, or drops it, logs EventMailDropped
// and reports false when the queue is full. It never blocks.
func (o *outbox) enqueue(ctx context.Context, job outboxJob) bool {
	select {
	case o.queue <- job:
		return true
	default:
		o.log(ctx, EventMailDropped, slog.String("purpose", string(job.purpose)))
		return false
	}
}

func (o *outbox) start(ctx context.Context) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	// The application context ends when shutdown begins; the worker's own
	// context lasts until Stop's deadline instead.
	worker, cancel := context.WithCancel(context.WithoutCancel(ctx))
	o.stop, o.done, o.cancel = make(chan struct{}), make(chan struct{}), cancel
	go o.run(worker, o.stop, o.done)
	return nil
}

func (o *outbox) run(ctx context.Context, stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	for {
		select {
		case job := <-o.queue:
			o.deliver(ctx, job)
		case <-stop:
			for ctx.Err() == nil {
				select {
				case job := <-o.queue:
					o.deliver(ctx, job)
				default:
					return
				}
			}
			return
		}
	}
}

// shutdown lets the worker send what's queued until ctx's deadline, then
// cancels the send in progress and waits for the worker to return. It
// returns ctx's error when the deadline came first: what was still queued
// is lost.
func (o *outbox) shutdown(ctx context.Context) error {
	o.mu.Lock()
	stop, done, cancel := o.stop, o.done, o.cancel
	o.stop, o.done, o.cancel = nil, nil, nil
	o.mu.Unlock()
	if stop == nil {
		return nil
	}
	defer cancel()
	close(stop)
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		cancel()
		<-done // Senders return promptly once their context ends
		return ctx.Err()
	}
}

// deliver prepares and sends job's email, logging a failure. A failed
// preparation is logged by its class only: its text could carry anything
// the lookup touched.
func (o *outbox) deliver(ctx context.Context, job outboxJob) {
	purpose := slog.String("purpose", string(job.purpose))
	d, ok, err := job.prepare(ctx)
	if err != nil {
		o.log(ctx, EventMailFailed, purpose, slog.String("error", fmt.Sprintf("preparing the email failed (%T)", err)))
		return
	}
	if !ok {
		return
	}
	if err := o.sender.Send(ctx, d.message); err != nil {
		o.log(ctx, EventMailFailed, purpose, slog.String("error", redact(err.Error(), d)))
	}
}

func (o *outbox) log(ctx context.Context, event string, attrs ...slog.Attr) {
	observabilitysafe.Call(func() { o.logger.LogAttrs(ctx, slog.LevelError, event, attrs...) })
}

// redact removes from text the email's recipient, its text (whole and line
// by line) and its secret, so a Sender's error that echoes them can be
// logged. tanGO's own SMTP sender already keeps credentials out of its
// errors.
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

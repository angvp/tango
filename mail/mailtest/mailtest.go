// Package mailtest provides a mail.Sender for tests: it records the
// messages it is given instead of delivering them.
package mailtest

import (
	"context"
	"io"
	"sync"

	"github.com/angvp/tango/mail"
)

// Sender records every message it sends. It rejects an invalid message with
// mail.ErrInvalidMessage, exactly as a real sender would. When Err is set,
// Send returns it and records nothing. A Sender is safe for concurrent use;
// its zero value is ready to use.
type Sender struct {
	// Err, when set, is returned by every Send.
	Err error

	mu       sync.Mutex
	messages []mail.Message
}

// Send validates message, then records it, or returns Err.
func (s *Sender) Send(ctx context.Context, message mail.Message) error {
	if err := mail.WriterSender(io.Discard).Send(ctx, message); err != nil {
		return err
	}
	if s.Err != nil {
		return s.Err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, message)
	return nil
}

// Messages returns the messages sent so far, oldest first.
func (s *Sender) Messages() []mail.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]mail.Message(nil), s.messages...)
}

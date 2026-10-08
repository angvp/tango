// Package mail sends outgoing email. A [Message] is one plain-text email to
// one recipient, optionally with in-memory attachments; a [Sender] delivers
// it. [NewSMTPSender] delivers through an SMTP server that always encrypts
// (except to a loopback host), and [WriterSender] writes the message out for
// development. Every sender validates and encodes a message the same way, so
// what WriterSender prints is what SMTP sends. See ADR 0044 and
// https://tangoframework.com/docs/guides/mail/.
package mail

import (
	"context"
	"errors"
)

// Sender delivers a Message. Send is synchronous and truthful: it returns
// nil only once the message has been handed on, and otherwise the error
// that stopped it. An invalid message fails with ErrInvalidMessage before
// anything is sent.
type Sender interface {
	Send(ctx context.Context, message Message) error
}

// Message is one plain-text email to one recipient. From and To are single
// addresses, optionally with a display name ("Shop <noreply@example.com>").
type Message struct {
	From        string
	To          string
	Subject     string
	Text        string
	Attachments []Attachment
}

// Attachment is one file attached to a Message, held in memory. An empty
// ContentType is sent as application/octet-stream.
type Attachment struct {
	Filename    string
	ContentType string
	Data        []byte
}

// ErrInvalidMessage is returned for a message no sender will send: a
// missing or malformed address, or a line break in a header value.
var ErrInvalidMessage = errors.New("mail: invalid message")

// Option configures a sender.
type Option func(*options)

type options struct{}

func newOptions(opts []Option) options {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

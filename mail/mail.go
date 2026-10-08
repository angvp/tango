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
	"crypto/x509"
	"errors"
	"fmt"
	"time"
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

// ErrInvalidAttachment is returned for an attachment no sender will send:
// a missing filename, a control character in its filename, or a malformed
// content type. An error matching it also matches ErrInvalidMessage.
var ErrInvalidAttachment = errors.New("mail: invalid attachment")

// ErrMessageTooLarge is returned when a message's attachments together
// exceed the sender's limit (see WithMaxAttachmentSize). It is a configured
// policy, not a malformed message, so it doesn't match ErrInvalidMessage.
var ErrMessageTooLarge = errors.New("mail: message too large")

// DefaultMaxAttachmentSize is the most raw attachment bytes, all of a
// message's attachments together, a sender accepts unless configured
// otherwise: 10 MiB.
const DefaultMaxAttachmentSize = 10 << 20

// Option configures a sender.
type Option func(*options)

type options struct {
	maxAttachmentSize int64
	defaultTimeout    time.Duration
	rootCAs           *x509.CertPool // nil: the system's
}

// WithMaxAttachmentSize sets the most raw attachment bytes, all of a
// message's attachments together, a sender accepts; a larger message fails
// with ErrMessageTooLarge. On the wire, base64 makes attachments about a
// third larger. The limit bounds what is sent, not memory the caller has
// already allocated. It panics unless n is positive.
func WithMaxAttachmentSize(n int64) Option {
	if n <= 0 {
		panic(fmt.Sprintf("mail: WithMaxAttachmentSize needs a positive size, got %d", n))
	}
	return func(o *options) { o.maxAttachmentSize = n }
}

func newOptions(opts []Option) options {
	o := options{maxAttachmentSize: DefaultMaxAttachmentSize, defaultTimeout: DefaultSMTPTimeout}
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

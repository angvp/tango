package mail

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"time"
)

// encode validates message and returns it in wire format, with CRLF line
// endings. Every sender sends exactly these bytes.
func encode(message Message, _ options) ([]byte, error) {
	from, err := parseAddress("From", message.From)
	if err != nil {
		return nil, err
	}
	to, err := parseAddress("To", message.To)
	if err != nil {
		return nil, err
	}
	if hasLineBreak(message.Subject) {
		return nil, fmt.Errorf("%w: Subject contains a line break", ErrInvalidMessage)
	}

	var b bytes.Buffer
	header := func(name, value string) { fmt.Fprintf(&b, "%s: %s\r\n", name, value) }
	header("From", from.String())
	header("To", to.String())
	header("Subject", mime.QEncoding.Encode("utf-8", message.Subject))
	header("Date", time.Now().Format(time.RFC1123Z))
	header("Message-ID", messageID(from.Address))
	header("MIME-Version", "1.0")
	header("Content-Type", "text/plain; charset=utf-8")
	header("Content-Transfer-Encoding", "quoted-printable")
	b.WriteString("\r\n")
	writeText(&b, message.Text)
	return b.Bytes(), nil
}

// parseAddress parses one address for header field, refusing line breaks
// (header injection) and lists.
func parseAddress(field, raw string) (*mail.Address, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("%w: %s is empty", ErrInvalidMessage, field)
	}
	if hasLineBreak(raw) {
		return nil, fmt.Errorf("%w: %s contains a line break", ErrInvalidMessage, field)
	}
	address, err := mail.ParseAddress(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %s is not one address", ErrInvalidMessage, field)
	}
	if hasLineBreak(address.Name) {
		return nil, fmt.Errorf("%w: %s's name contains a line break", ErrInvalidMessage, field)
	}
	return address, nil
}

func hasLineBreak(s string) bool { return strings.ContainsAny(s, "\r\n") }

// writeText writes text quoted-printable, with CRLF line endings.
func writeText(b *bytes.Buffer, text string) {
	w := quotedprintable.NewWriter(b)
	_, _ = w.Write([]byte(strings.ReplaceAll(text, "\r\n", "\n")))
	_ = w.Close()
}

// messageID returns a unique Message-ID at the sender's domain.
func messageID(fromAddress string) string {
	random := make([]byte, 16)
	_, _ = rand.Read(random)
	domain := fromAddress[strings.LastIndexByte(fromAddress, '@')+1:]
	return "<" + hex.EncodeToString(random) + "@" + domain + ">"
}

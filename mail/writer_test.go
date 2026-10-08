package mail_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"net/mail"
	"strings"
	"testing"

	tangomail "github.com/angvp/tango/mail"
)

// readMessage parses what a WriterSender wrote.
func readMessage(t *testing.T, raw []byte) *mail.Message {
	t.Helper()
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("not a valid message: %v\n%s", err, raw)
	}
	return msg
}

func TestWriterSenderWritesAPlainTextMessage(t *testing.T) {
	tests := []struct {
		name, subject, text string
	}{
		{"ASCII", "Reset your password", "Follow this link:\nhttps://example.com/reset/\n"},
		{"non-ASCII", "Réinitialisez votre mot de passe ✓", "Suivez ce lien : ça marche.\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			sender := tangomail.WriterSender(&out)
			err := sender.Send(context.Background(), tangomail.Message{
				From: "Shop <noreply@example.com>", To: "alice@example.com", Subject: tt.subject, Text: tt.text,
			})
			if err != nil {
				t.Fatal(err)
			}
			msg := readMessage(t, out.Bytes())
			if from, err := msg.Header.AddressList("From"); err != nil || from[0].Address != "noreply@example.com" || from[0].Name != "Shop" {
				t.Fatalf("From = %v, %v", from, err)
			}
			if to, err := msg.Header.AddressList("To"); err != nil || to[0].Address != "alice@example.com" {
				t.Fatalf("To = %v, %v", to, err)
			}
			subject, err := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
			if err != nil || subject != tt.subject {
				t.Fatalf("Subject = %q, %v; want %q", subject, err, tt.subject)
			}
			if mediaType, params, _ := mime.ParseMediaType(msg.Header.Get("Content-Type")); mediaType != "text/plain" || params["charset"] != "utf-8" {
				t.Fatalf("Content-Type = %q", msg.Header.Get("Content-Type"))
			}
			if msg.Header.Get("Content-Transfer-Encoding") != "quoted-printable" {
				t.Fatalf("Content-Transfer-Encoding = %q", msg.Header.Get("Content-Transfer-Encoding"))
			}
			if text := decodeQP(t, msg.Body); text != tt.text {
				t.Fatalf("text = %q, want %q", text, tt.text)
			}
		})
	}
}

func TestInvalidMessagesAreRejectedBeforeAnythingIsWritten(t *testing.T) {
	valid := tangomail.Message{From: "noreply@example.com", To: "alice@example.com", Subject: "Hi", Text: "x"}
	tests := []struct {
		name   string
		change func(*tangomail.Message)
	}{
		{"empty From", func(m *tangomail.Message) { m.From = "" }},
		{"empty To", func(m *tangomail.Message) { m.To = "" }},
		{"unparsable To", func(m *tangomail.Message) { m.To = "not an address" }},
		{"two recipients", func(m *tangomail.Message) { m.To = "a@example.com, b@example.com" }},
		{"CRLF in Subject", func(m *tangomail.Message) { m.Subject = "Hi\r\nBcc: victim@example.com" }},
		{"LF in Subject", func(m *tangomail.Message) { m.Subject = "Hi\nBcc: victim@example.com" }},
		{"CRLF in To", func(m *tangomail.Message) { m.To = "alice@example.com\r\nBcc: victim@example.com" }},
		{"CR in display name", func(m *tangomail.Message) { m.From = "\"Shop\rBcc: x\" <noreply@example.com>" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			message := valid
			tt.change(&message)
			var out bytes.Buffer
			err := tangomail.WriterSender(&out).Send(context.Background(), message)
			if !errors.Is(err, tangomail.ErrInvalidMessage) {
				t.Fatalf("error = %v, want ErrInvalidMessage", err)
			}
			if out.Len() != 0 {
				t.Fatalf("wrote %q for an invalid message", out.String())
			}
		})
	}
}

func decodeQP(t *testing.T, body io.Reader) string {
	t.Helper()
	text, err := io.ReadAll(qpReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(text), "\r\n", "\n")
}

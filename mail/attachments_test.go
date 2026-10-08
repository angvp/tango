package mail_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"strings"
	"testing"

	tangomail "github.com/angvp/tango/mail"
)

// part is one decoded MIME part.
type part struct {
	contentType, disposition, filename string
	data                               []byte
}

// readParts parses a multipart/mixed message and decodes every part.
func readParts(t *testing.T, raw []byte) []part {
	t.Helper()
	msg := readMessage(t, raw)
	mediaType, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/mixed" {
		t.Fatalf("Content-Type = %q, want multipart/mixed", msg.Header.Get("Content-Type"))
	}
	reader := multipart.NewReader(msg.Body, params["boundary"])
	var parts []part
	for {
		p, err := reader.NextRawPart()
		if err == io.EOF {
			return parts
		}
		if err != nil {
			t.Fatal(err)
		}
		var body io.Reader = p
		switch encoding := p.Header.Get("Content-Transfer-Encoding"); encoding {
		case "base64":
			body = base64.NewDecoder(base64.StdEncoding, p)
		case "quoted-printable":
			body = qpReader(p)
		default:
			t.Fatalf("part encoding = %q", encoding)
		}
		data, err := io.ReadAll(body)
		if err != nil {
			t.Fatalf("decoding part: %v", err)
		}
		disposition, dispositionParams, _ := mime.ParseMediaType(p.Header.Get("Content-Disposition"))
		parts = append(parts, part{p.Header.Get("Content-Type"), disposition, dispositionParams["filename"], data})
	}
}

func send(t *testing.T, message tangomail.Message, opts ...tangomail.Option) ([]byte, error) {
	t.Helper()
	var out bytes.Buffer
	err := tangomail.WriterSender(&out, opts...).Send(context.Background(), message)
	return out.Bytes(), err
}

func withAttachments(attachments ...tangomail.Attachment) tangomail.Message {
	return tangomail.Message{From: "noreply@example.com", To: "alice@example.com", Subject: "Your invoice", Text: "Attached.\n", Attachments: attachments}
}

func TestAttachmentsAreSentAsMultipartMixed(t *testing.T) {
	pdf := bytes.Repeat([]byte{0x25, 0x50, 0x44, 0x46, 0x00, 0xff}, 40) // long enough to wrap
	raw, err := send(t, withAttachments(
		tangomail.Attachment{Filename: "invoice.pdf", ContentType: "application/pdf", Data: pdf},
		tangomail.Attachment{Filename: "notes.bin", Data: []byte("x")},
		tangomail.Attachment{Filename: "empty.txt", ContentType: "text/plain", Data: nil},
	))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(raw), "\r\n") {
		if len(line) > 78 {
			t.Fatalf("line longer than 78 characters: %q", line)
		}
	}
	parts := readParts(t, raw)
	if len(parts) != 4 {
		t.Fatalf("parts = %d, want the text and three attachments", len(parts))
	}
	if mediaType, params, _ := mime.ParseMediaType(parts[0].contentType); mediaType != "text/plain" || params["charset"] != "utf-8" || strings.ReplaceAll(string(parts[0].data), "\r\n", "\n") != "Attached.\n" {
		t.Fatalf("text part = %q %q", parts[0].contentType, parts[0].data)
	}
	want := []struct {
		filename, mediaType string
		data                []byte
	}{
		{"invoice.pdf", "application/pdf", pdf},
		{"notes.bin", "application/octet-stream", []byte("x")},
		{"empty.txt", "text/plain", nil},
	}
	for i, w := range want {
		got := parts[i+1]
		mediaType, _, _ := mime.ParseMediaType(got.contentType)
		if got.disposition != "attachment" || got.filename != w.filename || mediaType != w.mediaType || !bytes.Equal(got.data, w.data) {
			t.Fatalf("attachment %d = %s %q %q (%d bytes), want %q %q (%d bytes)", i, got.disposition, got.filename, mediaType, len(got.data), w.filename, w.mediaType, len(w.data))
		}
	}
}

func TestANonASCIIFilenameSurvives(t *testing.T) {
	raw, err := send(t, withAttachments(tangomail.Attachment{Filename: "facture été.pdf", Data: []byte("x")}))
	if err != nil {
		t.Fatal(err)
	}
	if got := readParts(t, raw)[1].filename; got != "facture été.pdf" {
		t.Fatalf("filename = %q", got)
	}
}

func TestAMessageWithoutAttachmentsStaysPlainText(t *testing.T) {
	raw, err := send(t, withAttachments())
	if err != nil {
		t.Fatal(err)
	}
	if mediaType, _, _ := mime.ParseMediaType(readMessage(t, raw).Header.Get("Content-Type")); mediaType != "text/plain" {
		t.Fatalf("Content-Type = %q, want text/plain", mediaType)
	}
}

func TestAnInvalidFilenameIsAnInvalidAttachment(t *testing.T) {
	for _, filename := range []string{"", "a\r\nContent-Type: text/html", "a\nb", "tab\there", "nul\x00", "del\x7f"} {
		raw, err := send(t, withAttachments(tangomail.Attachment{Filename: filename, Data: []byte("x")}))
		if !errors.Is(err, tangomail.ErrInvalidAttachment) || !errors.Is(err, tangomail.ErrInvalidMessage) {
			t.Fatalf("filename %q: error = %v, want ErrInvalidAttachment and ErrInvalidMessage", filename, err)
		}
		if len(raw) != 0 {
			t.Fatalf("filename %q: wrote a message", filename)
		}
	}
}

func TestAnInvalidContentTypeIsAnInvalidAttachment(t *testing.T) {
	_, err := send(t, withAttachments(tangomail.Attachment{Filename: "a.txt", ContentType: "text/plain\r\nX-Evil: 1", Data: []byte("x")}))
	if !errors.Is(err, tangomail.ErrInvalidAttachment) {
		t.Fatalf("error = %v, want ErrInvalidAttachment", err)
	}
}

func TestAttachmentsOverTheSizeLimitAreRefused(t *testing.T) {
	tests := []struct {
		name  string
		limit int64 // 0: the default
		sizes []int
		ok    bool
	}{
		{"default, at the limit", 0, []int{6 << 20, 4 << 20}, true},
		{"default, one byte over", 0, []int{6 << 20, 4<<20 + 1}, false},
		{"configured, at the limit", 100, []int{60, 40}, true},
		{"configured, one byte over", 100, []int{60, 41}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var attachments []tangomail.Attachment
			for i, size := range tt.sizes {
				attachments = append(attachments, tangomail.Attachment{Filename: string(rune('a'+i)) + ".bin", Data: make([]byte, size)})
			}
			var opts []tangomail.Option
			if tt.limit != 0 {
				opts = append(opts, tangomail.WithMaxAttachmentSize(tt.limit))
			}
			raw, err := send(t, withAttachments(attachments...), opts...)
			if tt.ok && err != nil {
				t.Fatalf("error = %v, want none", err)
			}
			if !tt.ok && (!errors.Is(err, tangomail.ErrMessageTooLarge) || errors.Is(err, tangomail.ErrInvalidMessage) || len(raw) != 0) {
				t.Fatalf("error = %v, wrote %d bytes; want ErrMessageTooLarge alone and nothing written", err, len(raw))
			}
		})
	}
}

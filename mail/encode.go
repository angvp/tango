package mail

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"strings"
	"time"
)

// encode validates message and returns it in wire format, with CRLF line
// endings. Every sender sends exactly these bytes.
func encode(message Message, o options) ([]byte, error) {
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
	attachments, err := checkAttachments(message.Attachments, o.maxAttachmentSize)
	if err != nil {
		return nil, err
	}

	var b bytes.Buffer
	header := func(name, value string) { fmt.Fprintf(&b, "%s: %s\r\n", name, value) }
	header("From", from.String())
	header("To", to.String())
	header("Subject", mime.QEncoding.Encode("utf-8", message.Subject))
	header("Date", time.Now().Format(time.RFC1123Z))
	header("Message-ID", messageID(from.Address))
	header("MIME-Version", "1.0")
	if len(attachments) == 0 {
		header("Content-Type", textContentType)
		header("Content-Transfer-Encoding", "quoted-printable")
		b.WriteString("\r\n")
		writeText(&b, message.Text)
		return b.Bytes(), nil
	}

	body := multipart.NewWriter(&b)
	// Folded, so the long boundary doesn't push the line past 78 characters.
	header("Content-Type", "multipart/mixed;\r\n boundary="+body.Boundary())
	b.WriteString("\r\n")
	text, _ := body.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {textContentType},
		"Content-Transfer-Encoding": {"quoted-printable"},
	})
	writeText(text, message.Text)
	for _, a := range attachments {
		part, _ := body.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {a.ContentType},
			"Content-Disposition":       {mime.FormatMediaType("attachment", map[string]string{"filename": a.Filename})},
			"Content-Transfer-Encoding": {"base64"},
		})
		writeBase64(part, a.Data)
	}
	_ = body.Close()
	return b.Bytes(), nil
}

const textContentType = "text/plain; charset=utf-8"

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

// checkAttachments validates attachments against the size limit and
// returns them with their content types normalized.
func checkAttachments(attachments []Attachment, limit int64) ([]Attachment, error) {
	checked := make([]Attachment, 0, len(attachments))
	var total int64
	for _, a := range attachments {
		if a.Filename == "" || strings.ContainsFunc(a.Filename, isControl) {
			return nil, invalidAttachment("a filename is empty or contains a control character")
		}
		contentType := "application/octet-stream"
		if a.ContentType != "" {
			mediaType, params, err := mime.ParseMediaType(a.ContentType)
			if err != nil || strings.ContainsFunc(a.ContentType, isControl) {
				return nil, invalidAttachment("a content type is malformed")
			}
			contentType = mime.FormatMediaType(mediaType, params)
		}
		total += int64(len(a.Data))
		if total > limit {
			return nil, fmt.Errorf("%w: attachments exceed %d bytes", ErrMessageTooLarge, limit)
		}
		checked = append(checked, Attachment{Filename: a.Filename, ContentType: contentType, Data: a.Data})
	}
	return checked, nil
}

func invalidAttachment(reason string) error {
	return fmt.Errorf("%w: %w: %s", ErrInvalidMessage, ErrInvalidAttachment, reason)
}

func isControl(r rune) bool { return r < 0x20 || r == 0x7f }

func hasLineBreak(s string) bool { return strings.ContainsAny(s, "\r\n") }

// writeText writes text quoted-printable, with CRLF line endings.
func writeText(w io.Writer, text string) {
	qp := quotedprintable.NewWriter(w)
	_, _ = qp.Write([]byte(strings.ReplaceAll(text, "\r\n", "\n")))
	_ = qp.Close()
}

// base64LineLength is how many base64 characters each line carries, within
// RFC 2045's limit of 76.
const base64LineLength = 76

// writeBase64 writes data base64-encoded, in CRLF-terminated lines.
func writeBase64(w io.Writer, data []byte) {
	encoded := base64.StdEncoding.EncodeToString(data)
	for len(encoded) > base64LineLength {
		_, _ = io.WriteString(w, encoded[:base64LineLength]+"\r\n")
		encoded = encoded[base64LineLength:]
	}
	if encoded != "" {
		_, _ = io.WriteString(w, encoded+"\r\n")
	}
}

// messageID returns a unique Message-ID at the sender's domain.
func messageID(fromAddress string) string {
	random := make([]byte, 16)
	_, _ = rand.Read(random)
	domain := fromAddress[strings.LastIndexByte(fromAddress, '@')+1:]
	return "<" + hex.EncodeToString(random) + "@" + domain + ">"
}

package mail_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tangomail "github.com/angvp/tango/mail"
)

var invoice = tangomail.Message{From: "noreply@example.com", To: "alice@example.com", Subject: "Invoice", Text: "Your secret link: https://example.com/x\n"}

func TestSMTPSenderDeliversOverTLS(t *testing.T) {
	tests := []struct {
		name, scheme          string
		startTLS, implicitTLS bool
	}{
		{"STARTTLS", "smtp", true, false},
		{"implicit TLS", "smtps", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := smtpServer{startTLS: tt.startTLS, implicitTLS: tt.implicitTLS}
			pool := startSMTPServer(t, &server)
			sender, err := tangomail.NewSMTPSender(tt.scheme+"://mailer:s3cret@"+server.addr, tangomail.WithRootCAs(pool))
			if err != nil {
				t.Fatal(err)
			}
			if err := sender.Send(context.Background(), invoice); err != nil {
				t.Fatal(err)
			}
			got := server.snapshot()
			if len(got.data) != 1 || !strings.Contains(got.data[0], "Subject: Invoice") || !strings.Contains(got.data[0], "secret link") {
				t.Fatalf("server received %q", got.data)
			}
			if len(got.from) != 1 || !strings.Contains(got.from[0], "<noreply@example.com>") || !strings.Contains(got.to[0], "<alice@example.com>") {
				t.Fatalf("envelope = %q %q", got.from, got.to)
			}
			if len(got.auth) != 1 || got.auth[0] != "\x00mailer\x00s3cret" || !got.sawTLS[0] {
				t.Fatalf("auth = %q, over TLS = %v; want the URL's credentials over TLS", got.auth, got.sawTLS)
			}
		})
	}
}

func TestSMTPRequiresSTARTTLS(t *testing.T) {
	server := smtpServer{startTLS: false}
	pool := startSMTPServer(t, &server)
	sender, err := tangomail.NewSMTPSender("smtp://mailer:s3cret@"+server.addr, tangomail.WithRootCAs(pool))
	if err != nil {
		t.Fatal(err)
	}
	if err := sender.Send(context.Background(), invoice); err == nil {
		t.Fatal("sent without STARTTLS")
	}
	if got := server.snapshot(); len(got.auth) != 0 || len(got.from) != 0 || len(got.data) != 0 {
		t.Fatalf("server saw auth %q, MAIL %q, data %q; want nothing", got.auth, got.from, got.data)
	}
}

func TestSMTPRefusesAnUntrustedCertificate(t *testing.T) {
	server := smtpServer{startTLS: true}
	startSMTPServer(t, &server)
	sender, err := tangomail.NewSMTPSender("smtp://mailer:s3cret@" + server.addr)
	if err != nil {
		t.Fatal(err)
	}
	if err := sender.Send(context.Background(), invoice); err == nil {
		t.Fatal("sent to a server whose certificate isn't trusted")
	}
	if got := server.snapshot(); len(got.auth) != 0 || len(got.data) != 0 {
		t.Fatal("credentials or data reached an untrusted server")
	}
}

func TestPlaintextOnlyToLoopback(t *testing.T) {
	server := smtpServer{}
	startSMTPServer(t, &server)
	sender, err := tangomail.NewSMTPSender("smtp+insecure://" + server.addr)
	if err != nil {
		t.Fatal(err)
	}
	if err := sender.Send(context.Background(), invoice); err != nil {
		t.Fatal(err)
	}
	if got := server.snapshot(); len(got.data) != 1 || got.sawTLS[0] {
		t.Fatalf("data %d, TLS %v; want one plaintext delivery", len(got.data), got.sawTLS)
	}

	for _, rawURL := range []string{
		"smtp+insecure://mail.example.com:25",
		"smtp+insecure://10.0.0.5:1025",
		"smtp+insecure://user:pass@127.0.0.1:1025", // never credentials in plaintext
		"smtp+insecure://localhost.example.com:1025",
	} {
		if _, err := tangomail.NewSMTPSender(rawURL); err == nil {
			t.Fatalf("NewSMTPSender(%q) accepted plaintext", rawURL)
		}
	}
	for _, rawURL := range []string{"smtp+insecure://localhost:1025", "smtp+insecure://[::1]:1025"} {
		if _, err := tangomail.NewSMTPSender(rawURL); err != nil {
			t.Fatalf("NewSMTPSender(%q) = %v, want loopback accepted", rawURL, err)
		}
	}
}

func TestSMTPGivesUpOnAStalledServer(t *testing.T) {
	server := smtpServer{stall: true}
	startSMTPServer(t, &server)
	sender, err := tangomail.NewSMTPSender("smtp+insecure://"+server.addr, tangomail.WithDefaultTimeout(200*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	t.Run("default timeout", func(t *testing.T) {
		start := time.Now()
		if err := sender.Send(context.Background(), invoice); err == nil || time.Since(start) > 5*time.Second {
			t.Fatalf("error = %v after %s; want a timeout around 200ms", err, time.Since(start))
		}
	})
	t.Run("context deadline", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		slow, _ := tangomail.NewSMTPSender("smtp+insecure://"+server.addr, tangomail.WithDefaultTimeout(time.Hour))
		start := time.Now()
		if err := slow.Send(ctx, invoice); err == nil || time.Since(start) > 5*time.Second {
			t.Fatalf("error = %v after %s; want ctx's deadline to end it", err, time.Since(start))
		}
	})
}

func TestSMTPValidatesBeforeConnecting(t *testing.T) {
	server := smtpServer{}
	startSMTPServer(t, &server)
	sender, _ := tangomail.NewSMTPSender("smtp+insecure://" + server.addr)
	bad := invoice
	bad.To = "alice@example.com\r\nBcc: eve@example.com"
	if err := sender.Send(context.Background(), bad); !errors.Is(err, tangomail.ErrInvalidMessage) {
		t.Fatalf("error = %v, want ErrInvalidMessage", err)
	}
	if got := server.snapshot(); len(got.commands) != 0 {
		t.Fatalf("connected for an invalid message: %q", got.commands)
	}
}

func TestSMTPSenderFromEnv(t *testing.T) {
	t.Setenv("TANGO_SMTP_URL", "")
	if _, err := tangomail.SMTPSenderFromEnv(); !errors.Is(err, tangomail.ErrNotConfigured) {
		t.Fatalf("unset: error = %v, want ErrNotConfigured", err)
	}
	t.Setenv("TANGO_SMTP_URL", "smtps://mailer:s3cret@smtp.example.com")
	if _, err := tangomail.SMTPSenderFromEnv(); err != nil {
		t.Fatalf("set: error = %v", err)
	}
}

func TestABadSMTPURLIsRefusedWithoutEchoingIt(t *testing.T) {
	for _, rawURL := range []string{
		"http://mailer:hunter2@smtp.example.com",
		"smtp://mailer:hunter2@",
		"smtp://mailer:hunter2@smtp.example.com:notaport",
		"smtp://mailer:hunter2@smtp.example.com/%zz",
		"mailer:hunter2@smtp.example.com",
	} {
		_, err := tangomail.NewSMTPSender(rawURL)
		if err == nil {
			t.Fatalf("NewSMTPSender(%q) accepted it", rawURL)
		}
		if strings.Contains(err.Error(), "hunter2") {
			t.Fatalf("error %q contains the password", err)
		}
	}
}

func TestSMTPErrorsNeverContainCredentialsOrText(t *testing.T) {
	server := smtpServer{startTLS: false}
	startSMTPServer(t, &server)
	sender, _ := tangomail.NewSMTPSender("smtp://mailer:hunter2@" + server.addr)
	err := sender.Send(context.Background(), invoice)
	if err == nil || strings.Contains(err.Error(), "hunter2") || strings.Contains(err.Error(), "secret link") {
		t.Fatalf("error = %v", err)
	}
}

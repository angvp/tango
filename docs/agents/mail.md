# Agent Recipe: Outgoing Email

Use this when an app sends email, or tests code that does.

Canonical files: `mail/mail.go`, `mail/smtp.go`, `mail/writer.go`, and `mail/mailtest/mailtest.go`.

## Rule

- Depend on `mail.Sender` (`Send(ctx, mail.Message) error`), and construct the concrete sender in `main`.
- Production: `mail.SMTPSenderFromEnv()` reads `TANGO_SMTP_URL`, and returns `mail.ErrNotConfigured` when it's unset. Schemes:
  - `smtp://` requires STARTTLS;
  - `smtps://` is implicit TLS;
  - `smtp+insecure://` is plaintext, only to a loopback host, never with credentials, and only with an explicit port (`smtp+insecure://localhost:1025`).
- Development: choose `mail.WriterSender(os.Stdout)` explicitly. Never fall back to it automatically.
- A custom `Sender` must return promptly once `ctx` is done.
- Tests: use `&mailtest.Sender{}` and read `Messages()`; set `Err` to simulate a failing relay.
- `From` is application configuration, not an environment variable.

Tiny shape:

```go
sender, err := mail.SMTPSenderFromEnv()
if err != nil {
	return err
}
err = sender.Send(ctx, mail.Message{From: from, To: to, Subject: "Hi", Text: "…"})
```

## Don't

- Do not build email links from `Request.Host`; use a configured base URL.
- Do not log message text, recipients, links or SMTP URLs.
- Do not call `Send` inside a request when its timing could reveal something, such as whether an account exists. Queue the work instead, as `accounts` does.
- Do not expect HTML mail, several recipients, retries or queueing from `mail`.

## Check

- Invalid messages fail with `mail.ErrInvalidMessage`, attachments with `mail.ErrInvalidAttachment`, and oversized ones with `mail.ErrMessageTooLarge`.
- Read `docs/guides/mail.md`, and run `docs/agents/checklist.md`.

# Guide: sending email with `mail`

`github.com/angvp/tango/mail` sends outgoing email: one plain-text message to one recipient, optionally with attachments. It's what [`accounts`](accounts.md#password-reset-and-email-verification) uses for password reset and email verification, and any app can use it directly.

```go
sender, err := mail.SMTPSenderFromEnv()
if err != nil {
	return err // mail.ErrNotConfigured when TANGO_SMTP_URL is unset
}
err = sender.Send(ctx, mail.Message{
	From:    "Shop <noreply@example.com>",
	To:      "alice@example.com",
	Subject: "Your invoice",
	Text:    "Your invoice is attached.\n",
	Attachments: []mail.Attachment{
		{Filename: "invoice.pdf", ContentType: "application/pdf", Data: pdf},
	},
})
```

## Senders

A `mail.Sender` has one method, `Send(ctx, Message) error`. It's synchronous and truthful: it returns `nil` only once the message has been handed on. It must also return promptly once `ctx` is done, because `accounts` relies on that to finish within its shutdown deadline. Three come with tanGO:

- `mail.NewSMTPSender(url)`, or `mail.SMTPSenderFromEnv()` reading `TANGO_SMTP_URL`: an SMTP server.
- `mail.WriterSender(w)`: writes each message to `w` in the exact wire format SMTP would send. For development.
- `mailtest.Sender` from `mail/mailtest`: records messages for your tests, and rejects invalid ones exactly as a real sender does.

A provider with an HTTP API is a `Sender` you write yourself. See [ADR 0044](../adr/0044-mail-is-a-sender-interface-with-tls-required-smtp.md) for why `mail` is an interface.

## SMTP

The URL's scheme decides how the connection is secured:

| URL | Security | Default port |
|---|---|---|
| `smtp://user:pass@smtp.example.com` | STARTTLS, required: a server that doesn't offer it is refused before any credentials are sent | 587 |
| `smtps://user:pass@smtp.example.com` | TLS from the first byte | 465 |
| `smtp+insecure://localhost:1025` | Plaintext. Only for `localhost` or a loopback IP, never with credentials, and only with an explicit port | none: required |

`smtp+insecure` is for local tools such as [Mailpit](https://mailpit.axllent.org/); give their port, since they differ (1025, 2525, …). There's no plaintext mode for a real server. Each message opens one connection, bounded by the context's deadline, or 30 seconds when it has none. Errors never contain the URL, the credentials, the recipient or the message: when the server refuses a command, the error names the stage and reply code but not the reply's text, which can echo what tanGO sent, and any other failure names only the server and the stage. `errors.Is` still matches a timeout or cancellation.

Put `From` in your code or config, not in `TANGO_SMTP_URL`.

## Development

Nothing falls back to printing mail when SMTP isn't configured; choose a sender explicitly:

```go
var sender mail.Sender
if smtp, err := mail.SMTPSenderFromEnv(); err == nil {
	sender = smtp
} else if errors.Is(err, mail.ErrNotConfigured) {
	sender = mail.WriterSender(os.Stdout) // development only
} else {
	return err
}
```

Messages carry live links, such as password-reset links, so don't use `WriterSender` in production.

## Validation

Every sender validates a message the same way before sending anything:

- `From` and `To` must each be one address that `net/mail` can parse, optionally with a display name.
- A line break in `From`, `To` or `Subject` is rejected, which closes header injection.
- Both fail with `mail.ErrInvalidMessage`.

## Attachments

- A message with attachments is sent as `multipart/mixed`: the text first, then each attachment base64-encoded.
- `Filename` is required, and must not contain control characters.
- An empty `ContentType` is sent as `application/octet-stream`, and empty `Data` is fine.
- A bad attachment fails with an error matching both `mail.ErrInvalidAttachment` and `mail.ErrInvalidMessage`.

By default a message's attachments may total 10 MiB of raw bytes; `mail.WithMaxAttachmentSize(n)` changes that, and over the limit is `mail.ErrMessageTooLarge`. Base64 makes attachments about a third larger on the wire. The limit bounds what is sent, not the memory your `Data` already uses.

## Not included

HTML mail, inline images, streamed or file-path attachments, more than one recipient, DKIM signing, queueing and retries, delivery receipts and bounce handling. `accounts` queues its own emails; a `Send` that fails returns the error, and nothing retries it.

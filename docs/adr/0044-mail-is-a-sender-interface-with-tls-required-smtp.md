# Mail is a `Sender` interface with TLS-required SMTP

Password reset and email verification in `accounts` need outbound mail, and so will other apps. tanGO adds a `mail` package: a `Message` (one recipient, plain text, optional in-memory attachments), a `Sender` interface with one method, `Send(ctx, Message) error`, an SMTP sender, `WriterSender` for development, and a capturing sender in `mail/mailtest`.

**Why an interface, when ADRs 0029 and 0036 rejected them.** Those ADRs refused interfaces with one implementation and a speculative second. Mail has real alternatives from day one: an app's own provider HTTP API, a relay, and the test double every app sending mail needs. `Send` stays synchronous and truthful: it returns once the message is delivered to the next hop, or with the error that stopped it. Anything that needs a quick response, such as `accounts`, queues in front of a `Sender`; there is no `mail.Async` that would return before delivery.

**SMTP always encrypts, except to loopback.** `TANGO_SMTP_URL`, or the URL given to `NewSMTPSender`, picks the mode by scheme: `smtp://` requires STARTTLS and fails if the server doesn't offer it; `smtps://` uses TLS from the first byte. Plaintext needs its own scheme, `smtp+insecure://`, accepted only for `localhost` or a loopback IP, never with credentials, and only with an explicit port, for local tools such as Mailpit. Plaintext is a scheme of its own, rather than `smtp://` falling back on loopback, so that `smtp://` means STARTTLS everywhere. The standard library's SMTP client runs over a connection tanGO dials itself, so `ctx`'s deadline bounds the whole exchange (30 seconds when `ctx` has none).

**Errors are safe to log.** A server's reply can echo what the client sent, credentials included, so a refusal is reported by stage and reply code only, and other errors have the credentials, recipient and message removed. The error doesn't unwrap, so the original text can't be recovered, but `errors.Is` still matches its cause. A `Sender` must also return promptly once its context is done.

**No automatic development fallback.** Without SMTP configured, nothing falls back to logging mail. An application chooses `mail.WriterSender(os.Stdout)` explicitly. A silent fallback would print live reset links and attachment data into production logs the first time production was misconfigured.

**One encoder for every sender.** Validation (addresses, header injection, attachment filenames, the size limit) and MIME serialization live in one internal encoder that every sender uses, so the development output is the exact wire format production sends.

Rejected:

- **A concrete SMTP client with a test hook.** Apps sending through a provider's HTTP API would have to wrap or fork it.
- **Opportunistic TLS.** A network attacker can strip STARTTLS and read the credentials and reset links.
- **Bundled provider SDKs.** Providers that speak SMTP work through SMTP; the rest are an app's own `Sender`.

Consequences:

- `mail` and `mail/mailtest` join the Covered API, along with the MIME semantics: `multipart/mixed` when there are attachments, base64 attachments, and a quoted-printable UTF-8 text body. Boundaries and header order are not covered.
- No queueing, retries, HTML mail, inline images, streaming attachments or DKIM.

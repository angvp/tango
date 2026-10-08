# `accounts` password reset and email verification never reveal an account

With `accounts.WithMail(...)`, `accounts` gains password reset and email verification. Both send mail to an address someone typed, so both could tell an attacker whether that address has an account, and both put a secret in a link that leaves the app.

**One response, whatever the address.** Asking for a reset answers the same way for an existing, unknown or inactive address, or one in its cooldown. Registration still says "already registered" (ADR 0021); reset doesn't add a second oracle, and closing the first stays possible later.

**Mail goes through a bounded outbox.** An SMTP exchange takes seconds and an unknown address takes none, so sending inside the request would reveal through timing what the response hides. Each `accounts` app queues mail in memory (100 messages by default) for one worker registered as a Lifecycle component. A full queue drops the message and logs `tango.accounts.mail_dropped`; a failed send logs `tango.accounts.mail_failed`. Neither log carries an address, a token or a message. On shutdown the worker drains what its stop deadline allows; a crash loses the queue, and nothing is retried (ADR 0033).

**Tokens are rows, hashed, single-use and bound to an address.** One `AccountToken` model holds both kinds: the token's SHA-256, the account, its purpose, a hash of the address it was sent to, and an expiry (one hour for reset, 24 hours for verification). A stateless signed token couldn't be revoked or used only once (the ADR 0026 problem). Issuing a token removes the account's earlier ones of that purpose. A token works only while the account's email is still the address it was sent to, so a link sent before an operator changed the address can't take over the account or verify the new one. A completed reset removes every token and session the account has and sends the user to log in.

**GET never consumes anything.** Mail scanners and link previews follow links. The reset and verification pages show a form on GET; only POST sets a password or verifies. Pages carrying a token send `Referrer-Policy: no-referrer` and `Cache-Control: no-store`, and tokens stay in the query string, out of route patterns and logs.

**Links use a configured base URL.** `MailConfig.BaseURL` must be absolute `https://`, or `http://` on localhost. Links are never built from the request's `Host`, which an attacker controls.

**Verification doesn't gate login.** `Account.EmailVerifiedAt` records when the owner proved the address. An unverified account logs in as before, and an app that needs verification guards its Views with `accounts.RequireVerified`. Folding verification into `Active` would lock out every existing account the moment mail was enabled, since existing rows read as unverified.

Rejected:

- **Sending synchronously.** Timing reveals the account.
- **A goroutine per message.** Unbounded under a flood, and silently lost on shutdown.
- **Revealing the account on reset, like registration.** It adds an oracle with its own rate limits.

Consequences:

- Every `accounts` app migrates `Account.EmailVerifiedAt` and `AccountToken` on upgrade, mail or not, so enabling mail later is configuration only.
- Without `WithMail`, the new routes aren't mounted and `accounts` behaves as before.
- The cooldown (one email per address per purpose every five minutes) and the outbox are in-memory and per-process, the same limit as the login rate limiter.
- `accounts` has no email-change flow; whoever changes `Account.Email` clears `EmailVerifiedAt`.

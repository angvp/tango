# Accounts with mail

Password reset and email verification with the `accounts` app, run entirely on your machine. `mail.WriterSender(os.Stdout)` prints every email, with its live link, to the terminal instead of sending it, so you need no mail server. The home page is guarded by `accounts.RequireVerified`: only an account that has confirmed its email address sees it.

## Run it

```sh
git clone https://github.com/angvp/tango
cd tango/examples/accounts-mail
go run . -migrate
go run .
```

Then, in a browser:

1. **Register** at `http://localhost:8000/accounts/register/`. You're logged in, but `http://localhost:8000/` answers `403`: the address isn't confirmed yet.
2. **Find the verification email** in the terminal running the server. It contains a link to `/accounts/verify/?token=…`.
3. **Confirm**: open the link and press "Confirm my email". `http://localhost:8000/` now greets you.
4. **Reset the password** at `http://localhost:8000/accounts/password-reset/`. A second email appears in the terminal with a `/accounts/password-reset/confirm/?token=…` link.
5. **Choose a new password** through that link. Every session ends, so you're sent to the login page.
6. **Log in** with the new password at `http://localhost:8000/accounts/login/`.

`go test .` walks the same steps with `mailtest.Sender`, which records the emails instead of printing them.

## In Spanish

`catalog_es.go` is a Spanish catalog for every page, form error and email the `accounts` app produces, and `i18n.MiddlewareResolver(nil)` picks the language from `Accept-Language`. Open the pages with a browser set to Spanish (or `curl -H 'Accept-Language: es'`) and the pages, the errors and both emails come out in Spanish, links included; any other language gets the English you saw above. `i18n_test.go` runs the whole flow in Spanish and fails on any key the catalog lacks. See the [i18n guide](../../docs/guides/i18n.md) and [translating the pages](../../docs/guides/accounts.md#translating-the-pages).

## Configuration

- `TANGO_DB_DSN` picks the database and defaults to `sqlite://app.db`.
- The address is `TANGO_ADDR`, else the `PORT` hosting platforms set, else `:8000`.
- `BASE_URL` is the origin emailed links are built on. By default it's `http://localhost` on the port the app listens on; set it when the app is reached at another address.
- Ctrl-C or `SIGTERM` shuts the server down gracefully, sending any queued email first.

A real deployment passes `mail.SMTPSenderFromEnv()` instead of `mail.WriterSender`, and an `https://` `BASE_URL`. See [password reset and email verification](../../docs/guides/accounts.md#password-reset-and-email-verification) and the [mail guide](../../docs/guides/mail.md).

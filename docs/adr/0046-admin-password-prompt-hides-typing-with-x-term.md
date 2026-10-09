# The admin password prompt hides typing, using `golang.org/x/term`

`tango admin create` and `resetpassword` prompt for a password on stdin. The prompt used to read a plain line, so on a terminal the password showed on screen as it was typed, in scrollback and over a shoulder.

**Decision.** When stdin is a terminal, the prompt reads without echo using `golang.org/x/term`. A pipe or file is read as a line, as before, and `TANGO_ADMIN_PASSWORD` skips the prompt altogether.

**Why a new dependency in the root module.** `admin` lives in the root module, so `golang.org/x/term` joins it. Echo control needs a platform call (termios on Unix, console modes on Windows) that the standard library does not offer and that is easy to get wrong, notably restoring the terminal on every error path. `x/term` is the Go project's own package and builds on `golang.org/x/sys`, which the module already depends on. `go-isatty`, already a dependency, can detect a terminal but cannot turn echo off. Dropping the prompt in favour of environment-or-pipe only was rejected: it would remove the safe interactive path for people who run the command by hand.

**What stays untested.** The two calls into `x/term` sit behind a small struct that tests replace. The platform call itself is the only untested piece.

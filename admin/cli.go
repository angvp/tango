package admin

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"github.com/angvp/tango/db"
	"golang.org/x/term"
)

// HandleCLI scans args for the "tango admin" app-side flags
// (-tango-admin-create=<username>, -tango-admin-resetpassword=<username>,
// -tango-admin-deactivate=<username>, -tango-admin-grant-staff=<username>,
// -tango-admin-revoke-staff=<username>, -tango-admin-grant-superuser=<username>,
// -tango-admin-revoke-superuser=<username>) and, if one is present, performs
// the corresponding account operation against store and reports
// handled=true. If none is present it returns handled=false, nil so the
// caller (a generated main.go) proceeds to its other flag handling / serving.
//
// Unlike tango.DispatchFlags, HandleCLI does not use the flag package: both
// functions are called with the same os.Args[1:] from a generated main.go,
// and flag.Parse errors out on any flag it doesn't itself define — it can't
// tolerate seeing the other dispatcher's flags. A manual scan lets each
// dispatcher recognize only its own flags and ignore the rest.
//
// create and resetpassword take the new password from TANGO_ADMIN_PASSWORD
// when it is set and non-empty, and otherwise read it from stdin. Neither is a
// flag value, so it never appears in a shell history or the process arguments.
// The environment is visible to process-inspection tools, so prefer the
// deployment platform's secret mechanism for long-lived values.
func HandleCLI(ctx context.Context, store *db.Store, args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) (bool, error) {
	if username, ok := flagValue(args, "-tango-admin-create"); ok {
		password, err := newPassword(stdin, stdout)
		if err != nil {
			return true, err
		}
		var opts []AccountOption
		if flagPresent(args, "-tango-admin-no-staff") {
			opts = append(opts, WithoutStaff())
		}
		if flagPresent(args, "-tango-admin-no-superuser") {
			opts = append(opts, WithoutSuperuser())
		}
		if err := CreateAccount(ctx, store, username, password, opts...); err != nil {
			return true, err
		}
		fmt.Fprintf(stdout, "created admin account %q\n", username)
		return true, nil
	}
	if username, ok := flagValue(args, "-tango-admin-resetpassword"); ok {
		password, err := newPassword(stdin, stdout)
		if err != nil {
			return true, err
		}
		if err := ResetPassword(ctx, store, username, password); err != nil {
			return true, err
		}
		fmt.Fprintf(stdout, "reset password for admin account %q\n", username)
		return true, nil
	}
	if username, ok := flagValue(args, "-tango-admin-deactivate"); ok {
		if err := Deactivate(ctx, store, username); err != nil {
			return true, err
		}
		fmt.Fprintf(stdout, "deactivated admin account %q\n", username)
		return true, nil
	}
	for _, verb := range []struct {
		flag    string
		action  func(context.Context, *db.Store, string) error
		message string
	}{
		{"-tango-admin-grant-staff", GrantStaff, "granted staff access to admin account %q\n"},
		{"-tango-admin-revoke-staff", RevokeStaff, "revoked staff access from admin account %q\n"},
		{"-tango-admin-grant-superuser", GrantSuperuser, "granted superuser access to admin account %q\n"},
		{"-tango-admin-revoke-superuser", RevokeSuperuser, "revoked superuser access from admin account %q\n"},
	} {
		if username, ok := flagValue(args, verb.flag); ok {
			if err := verb.action(ctx, store, username); err != nil {
				return true, err
			}
			fmt.Fprintf(stdout, verb.message, username)
			return true, nil
		}
	}
	return false, nil
}

// flagValue reports whether args contains "-name=value" or "--name=value"
// (the only form tanGO's own CLI emits) and returns value if so.
func flagValue(args []string, name string) (string, bool) {
	for _, arg := range args {
		for _, prefix := range []string{name + "=", "-" + name + "="} {
			if strings.HasPrefix(arg, prefix) {
				return strings.TrimPrefix(arg, prefix), true
			}
		}
	}
	return "", false
}

// flagPresent reports whether args contains name or "-"+name as a bare,
// valueless flag (the form a presence/opt-out flag like
// -tango-admin-no-staff takes, unlike flagValue's "-name=value" flags).
func flagPresent(args []string, name string) bool {
	for _, arg := range args {
		if arg == name || arg == "-"+name {
			return true
		}
	}
	return false
}

// passwordEnvVar names the environment variable that supplies the password for
// create and resetpassword without a prompt, for CI and containers.
const passwordEnvVar = "TANGO_ADMIN_PASSWORD"

// newPassword returns the password from TANGO_ADMIN_PASSWORD when it is set
// and non-empty, and otherwise prompts on stdin. It tells the operator which
// source it used but never prints the password itself.
func newPassword(stdin io.Reader, stdout io.Writer) (string, error) {
	if password := os.Getenv(passwordEnvVar); password != "" {
		fmt.Fprintf(stdout, "using the password from %s\n", passwordEnvVar)
		return password, nil
	}
	return readPassword(stdin, stdout, "New password: ")
}

// terminalOps is the part of reading a password that needs a real terminal;
// tests replace it. The defaults are thin wrappers over golang.org/x/term.
type terminalOps struct {
	isTerminal   func(fd int) bool
	readPassword func(fd int) ([]byte, error)
}

var terminal = terminalOps{isTerminal: term.IsTerminal, readPassword: readPasswordNoEcho}

// exitInterrupted is the conventional exit status of a process ended by Ctrl-C.
const exitInterrupted = 130

// readPasswordNoEcho wraps term.ReadPassword. That function keeps Ctrl-C
// enabled, so an interrupt would end the process before its deferred restore
// ran and leave the terminal without echo. Catch the interrupt, restore the
// saved terminal state, then exit as an interrupted process does. It is the
// platform call and is not covered by tests.
func readPasswordNoEcho(fd int) ([]byte, error) {
	saved, err := term.GetState(fd)
	if err != nil {
		return nil, err
	}
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	done := make(chan struct{})
	defer func() { signal.Stop(interrupt); close(done) }()
	go func() {
		select {
		case <-interrupt:
			_ = term.Restore(fd, saved)
			fmt.Fprintln(os.Stderr)
			os.Exit(exitInterrupted)
		case <-done:
		}
	}()
	return term.ReadPassword(fd)
}

// readPassword prompts and reads one line. When stdin is a terminal the typed
// characters are not echoed; any other stdin (a pipe, a file) is read as a line.
func readPassword(stdin io.Reader, stdout io.Writer, prompt string) (string, error) {
	fmt.Fprint(stdout, prompt)
	if f, ok := stdin.(*os.File); ok && terminal.isTerminal(int(f.Fd())) {
		password, err := terminal.readPassword(int(f.Fd()))
		// The Enter key was not echoed, so end the prompt line ourselves.
		fmt.Fprintln(stdout)
		if err != nil {
			return "", err
		}
		return string(password), nil
	}
	scanner := bufio.NewScanner(stdin)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", err
		}
		return "", fmt.Errorf("tango admin: no password provided")
	}
	return scanner.Text(), nil
}

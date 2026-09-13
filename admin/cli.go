package admin

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/angvp/tango/db"
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
// create and resetpassword read the new password from stdin rather than a
// flag value, so it never appears in a shell history or process listing.
func HandleCLI(ctx context.Context, store *db.Store, args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) (bool, error) {
	if username, ok := flagValue(args, "-tango-admin-create"); ok {
		password, err := readPassword(stdin, stdout, "New password: ")
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
		password, err := readPassword(stdin, stdout, "New password: ")
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

func readPassword(stdin io.Reader, stdout io.Writer, prompt string) (string, error) {
	fmt.Fprint(stdout, prompt)
	scanner := bufio.NewScanner(stdin)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", err
		}
		return "", fmt.Errorf("tango admin: no password provided")
	}
	return scanner.Text(), nil
}

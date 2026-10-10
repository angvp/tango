// Package profiles is the host application's own data about an account: the
// username and display name a single-page client shows. accounts keeps
// Account free of them (ADR 0020); this app owns them, writes them in the
// same transaction that creates the account, and lets the client log in
// with the username.
package profiles

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/angvp/tango"
	"github.com/angvp/tango/accounts"
	"github.com/angvp/tango/admin"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/model"
)

// Profile belongs to one account. Username and DisplayName are bounded
// strings: tango:"varchar=n" makes Store.Create refuse a longer value
// (counted in characters, on SQLite as well as PostgreSQL), and the
// migration gives the column VARCHAR(n). Bio is not bounded: a bare string
// is text of any length. Preferences is JSON text, which the admin edits
// with its JSON widget; the server stores exactly what it is given.
type Profile struct {
	ID          int64  `tango:"pk"`
	AccountID   int64  `tango:"fk=Account,unique"`
	Username    string `tango:"varchar=30,unique"`
	DisplayName string `tango:"varchar=60"`
	Bio         string
	Preferences string
}

// meta is Profile's model metadata, for the Store calls the registration
// hook and the login resolver make before the registry exists.
var meta = func() model.ModelMeta {
	registry := model.NewRegistry()
	if err := registry.Register(Profile{}); err != nil {
		panic(err)
	}
	m, _ := registry.Get("Profile")
	return m
}()

// New is the profiles app: it registers the Profile model, and its admin
// page, where an operator edits Bio as Markdown and Preferences as JSON.
func New() tango.App {
	return tango.NewApp("profiles", func(registry *tango.Registry) error {
		if err := registry.Models().Register(Profile{}); err != nil {
			return err
		}
		return registry.Admin().Register(Profile{}, admin.Options{
			ListDisplay: []string{"Username", "DisplayName"},
			Search:      []string{"Username", "DisplayName"},
			// The account link is set when the profile is created at
			// registration; the admin shows it but never edits it. (A profile
			// created by hand in the admin therefore has no account.)
			ReadOnly: []string{"AccountID"},
			Widgets: map[string]admin.Widget{
				"Bio":         admin.Markdown(admin.WithMarkdownPreview(previewBio)),
				"Preferences": admin.JSON(),
			},
		})
	})
}

// registrationProfile is the "profile" object a client sends with its
// registration.
type registrationProfile struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Bio         string `json:"bio"`
}

// OnRegister creates the account's Profile inside the registration
// transaction. Any error it returns rolls the account back too, so there
// is never an account without its profile. A problem the client can fix is
// an accounts.FieldError, which answers 422 with the public field name; a
// value longer than its varchar=n limit comes back from Store.Create as a
// *db.ValueTooLongError, which accounts answers the same way.
func OnRegister(ctx context.Context, tx *db.Store, reg accounts.Registration) error {
	var in registrationProfile
	if len(reg.Profile) == 0 {
		return accounts.FieldError{Field: "username", Message: "is required"}
	}
	if err := json.Unmarshal(reg.Profile, &in); err != nil {
		return accounts.FieldError{Field: "profile", Message: "is malformed"}
	}
	in.Username = strings.TrimSpace(in.Username)
	switch {
	case in.Username == "":
		return accounts.FieldError{Field: "username", Message: "is required"}
	case strings.Contains(in.Username, "@"):
		// Login tells an email from a username by its "@", so a username
		// may never contain one.
		return accounts.FieldError{Field: "username", Message: "must not contain @"}
	}
	if in.DisplayName == "" {
		in.DisplayName = in.Username
	}
	profile := Profile{AccountID: reg.AccountID, Username: in.Username, DisplayName: in.DisplayName, Bio: in.Bio}
	if err := tx.Create(ctx, meta, &profile); err != nil {
		if db.IsUniqueConstraintViolation(err) {
			return accounts.FieldError{Field: "username", Message: "is already taken"}
		}
		return err
	}
	return nil
}

// ResolveUsername finds the account behind a username, so login accepts
// either an email or a username.
func ResolveUsername(ctx context.Context, store *db.Store, username string) (int64, bool, error) {
	var rows []Profile
	query := db.Query{Where: []db.Condition{{Field: "Username", Op: db.OpEq, Value: username}}, Limit: 1}
	if err := store.List(ctx, meta, query, &rows); err != nil {
		return 0, false, err
	}
	if len(rows) == 0 {
		return 0, false, nil
	}
	return rows[0].AccountID, true, nil
}

// ForAccount returns the account's profile.
func ForAccount(ctx context.Context, store *db.Store, accountID int64) (Profile, bool, error) {
	var rows []Profile
	query := db.Query{Where: []db.Condition{{Field: "AccountID", Op: db.OpEq, Value: accountID}}, Limit: 1}
	if err := store.List(ctx, meta, query, &rows); err != nil {
		return Profile{}, false, err
	}
	if len(rows) == 0 {
		return Profile{}, false, nil
	}
	return rows[0], true, nil
}

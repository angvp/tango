package admin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/model"
)

// ErrAccountExists is returned by CreateAccount when the username is
// already taken.
var ErrAccountExists = errors.New("tango: admin account already exists")

// ErrAccountNotFound is returned by ResetPassword and Deactivate when no
// account with the given username exists.
var ErrAccountNotFound = errors.New("tango: admin account not found")

// adminModelMetas returns AdminUser/AdminSession's model metadata,
// independent of any project's own model registry — the tango admin CLI
// operates directly against store, without running full app registration.
func adminModelMetas() (userMeta model.ModelMeta, sessionMeta model.ModelMeta) {
	registry := model.NewRegistry()
	// AdminUser and AdminSession are known-good models (admin.New registers
	// them the same way at app registration time), so these errors cannot
	// occur here in practice.
	_ = registry.Register(AdminUser{})
	_ = registry.Register(AdminSession{})
	userMeta, _ = registry.Get("AdminUser")
	sessionMeta, _ = registry.Get("AdminSession")
	return userMeta, sessionMeta
}

// findAdminUserByUsername returns the AdminUser row for username, or
// ok=false if none exists.
func findAdminUserByUsername(ctx context.Context, store *db.Store, username string) (AdminUser, bool, error) {
	meta, _ := adminModelMetas()
	var rows []AdminUser
	sqlQuery := fmt.Sprintf(
		"SELECT %s AS ID, %s AS Username, %s AS PasswordHash, %s AS Active, %s AS IsStaff, %s AS IsSuperuser, %s AS CreatedAt FROM %s WHERE %s = ?",
		db.ColumnName("ID"), db.ColumnName("Username"), db.ColumnName("PasswordHash"),
		db.ColumnName("Active"), db.ColumnName("IsStaff"), db.ColumnName("IsSuperuser"),
		db.ColumnName("CreatedAt"), db.ColumnName(meta.Name), db.ColumnName("Username"),
	)
	if err := store.Query(ctx, &rows, sqlQuery, username); err != nil {
		return AdminUser{}, false, err
	}
	if len(rows) == 0 {
		return AdminUser{}, false, nil
	}
	return rows[0], true, nil
}

// AccountOption customizes a new admin account's IsStaff/IsSuperuser flags
// at creation time. CreateAccount defaults both to true — "create an
// immediately usable full admin account" is its long-standing behavior — so
// these are opt-outs, not opt-ins. See ADR 0019.
type AccountOption func(*AdminUser)

// WithoutStaff creates the account with IsStaff=false instead of the
// default true.
func WithoutStaff() AccountOption {
	return func(u *AdminUser) { u.IsStaff = false }
}

// WithoutSuperuser creates the account with IsSuperuser=false instead of
// the default true.
func WithoutSuperuser() AccountOption {
	return func(u *AdminUser) { u.IsSuperuser = false }
}

// CreateAccount creates a new Admin account with a bcrypt-hashed password.
// It is the only code path in tanGO that accepts a raw password and turns
// it into a stored credential — see the "tango admin create" CLI command.
// The account defaults to IsStaff=true, IsSuperuser=true; pass WithoutStaff
// and/or WithoutSuperuser to opt out of either.
func CreateAccount(ctx context.Context, store *db.Store, username string, password string, opts ...AccountOption) error {
	_, exists, err := findAdminUserByUsername(ctx, store, username)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("%w: %q", ErrAccountExists, username)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	meta, _ := adminModelMetas()
	user := AdminUser{
		Username:     username,
		PasswordHash: string(hash),
		Active:       true,
		IsStaff:      true,
		IsSuperuser:  true,
		CreatedAt:    time.Now().UTC(),
	}
	for _, opt := range opts {
		opt(&user)
	}
	return store.Create(ctx, meta, &user)
}

// ResetPassword hashes and stores a new password for an existing account,
// and invalidates every existing session for it.
func ResetPassword(ctx context.Context, store *db.Store, username string, password string) error {
	user, exists, err := findAdminUserByUsername(ctx, store, username)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("%w: %q", ErrAccountNotFound, username)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	meta, _ := adminModelMetas()
	user.PasswordHash = string(hash)
	if err := store.Update(ctx, meta, &user); err != nil {
		return err
	}
	return invalidateSessions(ctx, store, user.ID)
}

// Deactivate disables an account (without deleting its row, preserving
// audit history) and invalidates every existing session for it.
func Deactivate(ctx context.Context, store *db.Store, username string) error {
	user, exists, err := findAdminUserByUsername(ctx, store, username)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("%w: %q", ErrAccountNotFound, username)
	}

	meta, _ := adminModelMetas()
	user.Active = false
	if err := store.Update(ctx, meta, &user); err != nil {
		return err
	}
	return invalidateSessions(ctx, store, user.ID)
}

// GrantStaff sets IsStaff=true on an existing account, leaving Active,
// PasswordHash, and IsSuperuser untouched. Unlike Deactivate, this does not
// invalidate existing sessions: IsStaff is checked fresh from the database
// on every request (not cached in the session), so a revoked account is
// denied on its very next request regardless.
func GrantStaff(ctx context.Context, store *db.Store, username string) error {
	return setAccountFlag(ctx, store, username, func(u *AdminUser) { u.IsStaff = true })
}

// RevokeStaff sets IsStaff=false on an existing account. See GrantStaff for
// why this does not invalidate existing sessions.
func RevokeStaff(ctx context.Context, store *db.Store, username string) error {
	return setAccountFlag(ctx, store, username, func(u *AdminUser) { u.IsStaff = false })
}

// GrantSuperuser sets IsSuperuser=true on an existing account. See
// GrantStaff for why this does not invalidate existing sessions.
func GrantSuperuser(ctx context.Context, store *db.Store, username string) error {
	return setAccountFlag(ctx, store, username, func(u *AdminUser) { u.IsSuperuser = true })
}

// RevokeSuperuser sets IsSuperuser=false on an existing account. See
// GrantStaff for why this does not invalidate existing sessions.
func RevokeSuperuser(ctx context.Context, store *db.Store, username string) error {
	return setAccountFlag(ctx, store, username, func(u *AdminUser) { u.IsSuperuser = false })
}

// setAccountFlag loads an existing account by username, applies mutate to
// it, and persists the result. It is the shared implementation behind
// GrantStaff/RevokeStaff/GrantSuperuser/RevokeSuperuser.
func setAccountFlag(ctx context.Context, store *db.Store, username string, mutate func(*AdminUser)) error {
	user, exists, err := findAdminUserByUsername(ctx, store, username)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("%w: %q", ErrAccountNotFound, username)
	}

	mutate(&user)
	meta, _ := adminModelMetas()
	return store.Update(ctx, meta, &user)
}

// invalidateSessions deletes every AdminSession row belonging to userID.
func invalidateSessions(ctx context.Context, store *db.Store, userID int64) error {
	_, sessionMeta := adminModelMetas()

	var sessions []AdminSession
	sqlQuery := fmt.Sprintf(
		"SELECT %s AS ID FROM %s WHERE %s = ?",
		db.ColumnName("ID"), db.ColumnName(sessionMeta.Name), db.ColumnName("UserID"),
	)
	if err := store.Query(ctx, &sessions, sqlQuery, userID); err != nil {
		return err
	}
	for _, session := range sessions {
		if err := store.Delete(ctx, sessionMeta, session.ID); err != nil && !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, db.ErrNotFound) {
			return err
		}
	}
	return nil
}

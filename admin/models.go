package admin

import "time"

// AdminUser is an Admin account: a single authenticatable admin identity.
// Its PasswordHash is only ever written by the "tango admin" CLI family
// (create/resetpassword), which is the sole path that produces a valid
// bcrypt hash — no other code path accepts a raw password for this field.
// AdminUser is registered as an ordinary tanGO model (picked up by
// "tango makemigrations" like any project model) but is never registered
// with the admin's own CRUD registry, so it never appears in the generic
// admin UI.
type AdminUser struct {
	ID           int64  `tango:"pk"`
	Username     string `tango:"unique"`
	PasswordHash string
	Active       bool
	CreatedAt    time.Time
}

// AdminSession is one active login for an AdminUser: a session token,
// which AdminUser it belongs to, and a fixed expiry. Deleting a session's
// row logs out that one login without affecting the AdminUser it
// belongs to. Like AdminUser, it is registered as an ordinary model but
// never exposed through the admin's own CRUD UI.
type AdminSession struct {
	ID        int64  `tango:"pk"`
	Token     string `tango:"unique"`
	UserID    int64  `tango:"index"`
	ExpiresAt time.Time
}

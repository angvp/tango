// Package documents is the host application's own data about the files its
// accounts upload. tanGO's storage package keeps the bytes and returns a
// generated key; this app keeps the key and the metadata in its own model,
// decides who may download a file, and decides what happens to the bytes
// when a row goes away. Nothing here is a tanGO model field: a file is a
// row the host owns plus an object the host stores.
package documents

import (
	"time"

	"github.com/angvp/tango"
	"github.com/angvp/tango/accounts"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/storage"
)

// Document is one uploaded file. Key is the opaque key storage.Upload
// generated: it is never shown to the client, never a path and never an
// authorization decision (the owner check is OwnerID). Name is the client's
// display filename, kept as plain metadata. The bounded strings are
// tango:"varchar=n" columns, validated in runes by Store.Create.
type Document struct {
	ID          int64  `tango:"pk"`
	OwnerID     int64  `tango:"fk=Account,index"`
	Key         string `tango:"varchar=32,unique"`
	Name        string `tango:"varchar=255"`
	ContentType string `tango:"varchar=100"`
	Size        int64
	SHA256      string `tango:"varchar=64"`
	CreatedAt   time.Time
}

// Limits are the host's rules for an upload.
type Limits struct {
	// MaxFile is the most bytes one file may hold.
	MaxFile int64
	// Allowed lists the sniffed types accepted, such as "image/*".
	Allowed []string
	// Inline lists the types a download may display in the browser when
	// asked; every other type is always an attachment.
	Inline []string
}

// DefaultLimits accepts images and PDFs up to 4 MiB and shows only images
// inline.
func DefaultLimits() Limits {
	return Limits{
		MaxFile: 4 << 20,
		Allowed: []string{"image/png", "image/jpeg", "image/gif", "application/pdf"},
		Inline:  []string{"image/png", "image/jpeg", "image/gif"},
	}
}

// New is the documents app, keeping object bytes in objects. Its routes use
// the account session cookie accounts configured (the default name).
func New(store *db.Store, objects storage.Store, limits Limits) tango.App {
	return tango.NewApp("documents", func(registry *tango.Registry) error {
		if err := registry.Models().Register(Document{}); err != nil {
			return err
		}
		meta, _ := registry.Models().Get("Document")
		h := &handlers{store: store, meta: meta, objects: objects, limits: limits}
		return registry.Routes().Include("/documents/", h.routes())
	})
}

// currentAccountID is who the request's session cookie names.
func currentAccountID(ctx *tango.Context, store *db.Store) (int64, bool, error) {
	return accounts.CurrentAccountID(ctx, store, accounts.DefaultSessionCookieName)
}

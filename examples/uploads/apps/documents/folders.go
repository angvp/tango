package documents

import (
	"fmt"
	"html/template"

	"github.com/angvp/tango"
	"github.com/angvp/tango/admin"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/storage"
)

// Folder groups documents and has a cover image that staff upload from the
// admin form. Cover holds the object's generated key and nothing else: the
// admin widget stores the file and writes the key, and the host decides how
// it is served. The key is 32 characters, so the column is varchar=32.
type Folder struct {
	ID    int64  `tango:"pk"`
	Name  string `tango:"varchar=100"`
	Cover string `tango:"varchar=32"`
}

// coverRules accepts small images only.
var coverRules = storage.PutOptions{
	MaxSize:      1 << 20,
	AllowedTypes: []string{"image/png", "image/jpeg"},
}

// registerFolders puts Folder in the admin with a file field for its cover,
// stored in objects, and a staff-only Storage page showing how many
// documents the site holds. Both reuse the admin's sign-in and look.
func registerFolders(registry *tango.Registry, store *db.Store, objects storage.Store) error {
	if err := registry.Models().Register(Folder{}); err != nil {
		return err
	}
	if err := registry.Admin().Register(Folder{}, admin.Options{
		ListDisplay: []string{"Name", "Cover"},
		Widgets: map[string]admin.Widget{
			"Cover": admin.File(admin.FileOptions{Put: coverRules, Store: objects}),
		},
	}); err != nil {
		return err
	}
	return admin.RegisterPage(registry, admin.Page{
		Path:  "storage",
		Title: "Storage",
		Nav:   true,
		View:  storagePage(registry, store),
	})
}

// storagePage reports how many documents are stored, for the signed-in
// staff user.
func storagePage(registry *tango.Registry, store *db.Store) tango.View {
	return func(ctx *tango.Context) error {
		meta, ok := registry.Models().Get("Document")
		if !ok {
			return fmt.Errorf("documents: Document is not registered")
		}
		count, err := store.Count(ctx.Context(), meta, db.Query{})
		if err != nil {
			return err
		}
		user, _ := admin.CurrentUser(ctx)
		body := template.HTML(fmt.Sprintf("<p>%s, the site holds %d documents.</p>",
			template.HTMLEscapeString(user), count))
		return admin.RenderPage(ctx, "Storage", body)
	}
}

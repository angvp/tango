package documents

import (
	"context"
	"strconv"

	"github.com/angvp/tango"
	"github.com/angvp/tango/admin"
)

// registerAdmin lists every document in the admin changelist with its size,
// a shortened digest and a download link. The link goes through the same
// View as the API, so the owner check still decides who gets the file.
func registerAdmin(registry *tango.Registry) error {
	return registry.Admin().Register(Document{}, admin.Options{
		ListDisplay: []string{"Name", "Size", "SHA256", "Download"},
		Search:      []string{"Name"},
		Ordering:    []string{"-CreatedAt"},
		Labels:      map[string]string{"Download": "File"},
		// The key names the stored object and must never be shown or
		// repointed; the metadata is written by the upload, not the form.
		Widgets:  map[string]admin.Widget{"Key": storedKeyWidget{}},
		ReadOnly: []string{"OwnerID", "ContentType", "Size", "SHA256", "CreatedAt"},
		Display: map[string]admin.DisplayFunc{
			"Size":     sizeCell,
			"SHA256":   hashCell,
			"Download": downloadCell,
		},
	})
}

func sizeCell(_ context.Context, row any) admin.Display {
	doc, ok := row.(*Document)
	if !ok {
		return admin.Display{}
	}
	return admin.Display{Text: admin.ByteSize(doc.Size)}
}

func hashCell(_ context.Context, row any) admin.Display {
	doc, ok := row.(*Document)
	if !ok {
		return admin.Display{}
	}
	return admin.Display{Text: admin.ShortHash(doc.SHA256)}
}

func downloadCell(_ context.Context, row any) admin.Display {
	doc, ok := row.(*Document)
	if !ok {
		return admin.Display{}
	}
	return admin.Display{Text: "Download", Href: downloadURL(doc.ID)}
}

// downloadURL is the owner-only download route in routes(). The admin's link
// points at it and nowhere else: the admin never reads the bytes, never sees
// the key, and does not authorize the download itself.
func downloadURL(id int64) string {
	return "/documents/" + strconv.FormatInt(id, 10) + "/download/"
}

package admin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"reflect"
	"sort"

	"github.com/angvp/tango"
	"github.com/angvp/tango/i18n"
	"github.com/angvp/tango/internal/adminregistry"
	"github.com/angvp/tango/internal/security"
	"github.com/angvp/tango/model"
	"github.com/angvp/tango/storage"
	"github.com/angvp/tango/storage/local"
)

const (
	// fallbackStorageDir is where files go when no store was set anywhere.
	fallbackStorageDir = "./storage"
	// fallbackStorageEnv overrides fallbackStorageDir.
	fallbackStorageEnv = "TANGO_STORAGE_DIR"
	bytesPerMiB        = 1 << 20
)

// fileField is one model field that takes a file, with its resolved store.
type fileField struct {
	put   storage.PutOptions
	store storage.Store
	url   func(key string) string
}

// fileFields are a model's file fields, by Go field name. The zero value and
// nil have none, and every method then does nothing.
type fileFields struct {
	byName map[string]fileField
	names  []string
}

// fallbackStore is the local store used when the host set none.
type fallbackStore struct {
	dir   string
	store storage.Store
	// fields are the file fields that use it, as "Model.Field".
	fields []string
}

// resolveFileFields finds the file fields of one admin model and gives each
// its store: the field's own, else the registry's, else a local directory.
// It fails at startup, naming the field, when a field cannot work.
func resolveFileFields(registry *tango.Registry, registration ModelRegistration, fallback *fallbackStore) (*fileFields, error) {
	var found *fileFields
	for _, field := range registration.Model.Fields {
		widget, ok := registration.Options.Widgets[field.Name].(FileWidget)
		if !ok || contains(registration.Options.ReadOnly, field.Name) {
			continue // a read-only field shows its value and takes no upload
		}
		name := registration.Model.Name + "." + field.Name
		rules := widget.FileRules()
		if field.Type.Kind() != reflect.String {
			return nil, fmt.Errorf("%w: %s is %s, and a file field must be a string", ErrInvalidFile, name, field.Type.Kind())
		}
		if rules.Put.MaxSize <= 0 {
			return nil, fmt.Errorf("%w: %s needs a positive Put.MaxSize", ErrInvalidFile, name)
		}
		store := rules.Store
		if store == nil {
			store = registryStore(registry)
		}
		if store == nil {
			var err error
			if store, err = fallback.open(); err != nil {
				return nil, fmt.Errorf("%w: %s has no store and the local fallback %q cannot be used: %v", ErrInvalidFile, name, fallback.dir, err)
			}
			fallback.fields = append(fallback.fields, name)
			slog.Warn("tango.admin.storage_local_fallback", "field", name, "dir", fallback.dir,
				"detail", "no object store was set; files go to a local directory. Call Registry.SetStorage or set FileOptions.Store for production")
		}
		urlOf := rules.URL
		if urlOf == nil {
			urlOf = publicURLOf(store)
		}
		if found == nil {
			found = &fileFields{byName: map[string]fileField{}}
		}
		found.byName[field.Name] = fileField{put: rules.Put, store: store, url: urlOf}
		found.names = append(found.names, field.Name)
	}
	return found, nil
}

func registryStore(registry *tango.Registry) storage.Store {
	store, _ := registry.Storage()
	return store
}

func publicURLOf(store storage.Store) func(string) string {
	publicURLer, ok := store.(storage.PublicURLer)
	if !ok {
		return nil
	}
	return func(key string) string {
		link, _ := publicURLer.PublicURL(key)
		return link
	}
}

func newFallbackStore() *fallbackStore {
	dir := os.Getenv(fallbackStorageEnv)
	if dir == "" {
		dir = fallbackStorageDir
	}
	return &fallbackStore{dir: dir}
}

// open returns the local store, building it once.
func (f *fallbackStore) open() (storage.Store, error) {
	if f.store != nil {
		return f.store, nil
	}
	store, err := local.New(f.dir)
	if err != nil {
		return nil, err
	}
	f.store = store
	return store, nil
}

// has reports whether the model has any file field.
func (ff *fileFields) has() bool { return ff != nil && len(ff.byName) > 0 }

// linkFor is the URL of a field's current file, or "".
func (ff *fileFields) linkFor(field, key string) string {
	if !ff.has() || key == "" {
		return ""
	}
	if f, ok := ff.byName[field]; ok && f.url != nil {
		return f.url(key)
	}
	return ""
}

// hasFileWidget reports whether any editable field of the model has a file
// widget, which decides the form's encoding.
func hasFileWidget(meta model.ModelMeta, opts adminregistry.Options) bool {
	for _, field := range meta.Fields {
		if _, ok := opts.Widgets[field.Name].(FileWidget); ok && !contains(opts.ReadOnly, field.Name) {
			return true
		}
	}
	return false
}

// submission is one create or edit POST, read.
type submission struct {
	// form holds what populateFromForm reads.
	form FieldValues
	// values are the text fields as typed, shown again on a refused save.
	values url.Values
	// stored are the objects this request stored, by field, with their stores.
	stored map[string]storedFile
	// refused are the field messages for files the store refused.
	refused map[string]string
}

type storedFile struct {
	key   string
	store storage.Store
}

// formValues is the form a file widget's Parse sees: the text fields plus the
// keys of the files stored from this request.
type formValues struct {
	url.Values
	keys map[string]string
}

func (f formValues) UploadedKey(field string) (string, bool) {
	key, ok := f.keys[field]
	return key, ok
}

var (
	errSubmissionCSRF = errors.New("admin: form token is missing or wrong")
	// errStoreFailed is a failure to read or store the form's files that is
	// not the user's to fix. It was logged.
	errStoreFailed = errors.New("admin: the files could not be stored")
)

// readSubmission reads the POST of a create or edit form. A model without file
// fields is read as an urlencoded form, as always; one with file fields is
// read as multipart with storage.ReadForm, whose CSRF check runs before any
// file is stored.
func readSubmission(ctx *tango.Context, files *fileFields) (submission, error) {
	r := ctx.Request()
	if !files.has() {
		if err := r.ParseForm(); err != nil {
			return submission{}, err
		}
		if !verifySessionCSRF(ctx) {
			return submission{}, errSubmissionCSRF
		}
		return submission{form: r.PostForm, values: r.PostForm}, nil
	}
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return submission{}, errSubmissionCSRF
	}
	options := storage.FormOptions{
		Files:      map[string]storage.PutOptions{},
		Stores:     map[string]storage.Store{},
		TokenField: csrfFieldName,
		CheckToken: func(value string) bool { return security.VerifyCSRFToken(value, cookie.Value) },
	}
	var anyStore storage.Store
	for name, f := range files.byName {
		options.Files[name] = f.put
		options.Stores[name] = f.store
		anyStore = f.store
	}
	form, err := storage.ReadForm(anyStore, r, options)
	switch {
	case errors.Is(err, storage.ErrFormToken):
		return submission{}, errSubmissionCSRF
	case errors.Is(err, storage.ErrFileRefused):
		return submission{values: form.Values, refused: files.refusalMessages(ctx.Context(), form.Refused)}, err
	case err != nil:
		ctx.Logger().Error("admin file form failed", "error", err)
		return submission{values: form.Values}, errStoreFailed
	}
	sub := submission{values: form.Values, stored: map[string]storedFile{}}
	keys := map[string]string{}
	for name, file := range form.Files {
		keys[name] = file.Key
		sub.stored[name] = storedFile{key: file.Key, store: files.byName[name].store}
	}
	sub.form = formValues{Values: form.Values, keys: keys}
	return sub, nil
}

// refusalMessages words each refused file for the field it came from.
func (ff *fileFields) refusalMessages(ctx context.Context, refused map[string]error) map[string]string {
	messages := make(map[string]string, len(refused))
	for name, err := range refused {
		switch {
		case errors.Is(err, storage.ErrTooLarge):
			limit := (ff.byName[name].put.MaxSize + bytesPerMiB - 1) / bytesPerMiB
			messages[name] = i18n.T(ctx, "admin.file.too_large", "This file is too large. The limit is %d MiB.", limit)
		default:
			messages[name] = i18n.T(ctx, "admin.file.type_not_allowed", "This kind of file is not allowed here.")
		}
	}
	return messages
}

// discard deletes what this request stored, after a failed save. A file that
// cannot be deleted is logged and left, never hidden.
func (s submission) discard(ctx *tango.Context) {
	for field, file := range s.stored {
		deleteObject(ctx, file.store, file.key, field, "new")
	}
}

// retire deletes the files a saved row no longer points at: the old key of a
// replaced or removed file field. It runs only after the row is saved.
func (ff *fileFields) retire(ctx *tango.Context, before, after reflect.Value) {
	if !ff.has() || !before.IsValid() {
		return
	}
	names := append([]string(nil), ff.names...)
	sort.Strings(names)
	for _, name := range names {
		old := before.FieldByName(name).String()
		if old != "" && old != after.FieldByName(name).String() {
			deleteObject(ctx, ff.byName[name].store, old, name, "old")
		}
	}
}

func deleteObject(ctx *tango.Context, store storage.Store, key, field, which string) {
	if err := store.Delete(context.WithoutCancel(ctx.Context()), key); err != nil {
		ctx.Logger().Warn("admin could not delete a file object", "field", field, "which", which, "key", key, "error", err)
	}
}

// restore puts a file field's stored key back to what the row held before the
// request, in an instance whose save failed. The new object was deleted, so the
// form must not show it as the current file. before is invalid on create.
func (ff *fileFields) restore(instance, before reflect.Value) {
	if !ff.has() {
		return
	}
	for _, name := range ff.names {
		old := ""
		if before.IsValid() {
			old = before.FieldByName(name).String()
		}
		instance.FieldByName(name).SetString(old)
	}
}

// problem words a submission that could not be read as the form's field
// errors and form-level message. It reports false for any other error.
func (s submission) problem(ctx context.Context, err error) (map[string]string, string, bool) {
	switch {
	case errors.Is(err, storage.ErrFileRefused):
		return s.refused, "", true
	case errors.Is(err, errStoreFailed):
		return nil, i18n.T(ctx, "admin.file.store_failed", "The file could not be saved. Try again, or ask an administrator."), true
	}
	return nil, "", false
}

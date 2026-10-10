package documents

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/model"
	"github.com/angvp/tango/storage"
)

const (
	// jsonBodyLimit caps every route that carries no file. An upload route
	// has its own, larger limit: a body limit can be lowered by a route
	// closer to the View, never raised, so the file route is not under the
	// small one.
	jsonBodyLimit = 64 << 10
	// multipartOverhead is room for the multipart framing around a file.
	multipartOverhead = 64 << 10
)

type handlers struct {
	store   *db.Store
	meta    model.ModelMeta
	objects storage.Store
	limits  Limits
}

func (h *handlers) routes() tango.URLs {
	small := tango.Use(tango.MaxBodySize(jsonBodyLimit))
	return tango.URLs{
		tango.Path("GET", "/", h.list, tango.Name("list"), small),
		tango.Path("POST", "/", h.upload, tango.Name("upload"),
			tango.Use(tango.MaxBodySize(h.limits.MaxFile+multipartOverhead))),
		tango.Path("GET", "/{id}/download/", h.download, tango.Name("download"), small),
		tango.Path("DELETE", "/{id}/", h.remove, tango.Name("delete"), small),
	}
}

// view is the JSON a client sees. The storage key is not in it.
type view struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	ContentType string    `json:"content_type"`
	Size        int64     `json:"size"`
	SHA256      string    `json:"sha256"`
	CreatedAt   time.Time `json:"created_at"`
}

func viewOf(d Document) view {
	return view{ID: d.ID, Name: d.Name, ContentType: d.ContentType, Size: d.Size, SHA256: d.SHA256, CreatedAt: d.CreatedAt}
}

func (h *handlers) list(ctx *tango.Context) error {
	owner, ok, err := currentAccountID(ctx, h.store)
	if err != nil || !ok {
		return h.refuse(ctx, err)
	}
	var docs []Document
	err = h.store.List(ctx.Context(), h.meta, db.Query{
		Where:   []db.Condition{{Field: "OwnerID", Op: db.OpEq, Value: owner}},
		OrderBy: []string{"CreatedAt"},
	}, &docs)
	if err != nil {
		return err
	}
	out := make([]view, 0, len(docs))
	for _, d := range docs {
		out = append(out, viewOf(d))
	}
	return ctx.JSON(http.StatusOK, out)
}

// upload stores the bytes first, then the row. If the row cannot be
// written the new object is deleted, so a failure leaves neither behind.
func (h *handlers) upload(ctx *tango.Context) error {
	owner, ok, err := currentAccountID(ctx, h.store)
	if err != nil || !ok {
		return h.refuse(ctx, err)
	}
	file, err := storage.Upload(h.objects, ctx.Request(), storage.UploadOptions{
		PutOptions: storage.PutOptions{MaxSize: h.limits.MaxFile, AllowedTypes: h.limits.Allowed},
	})
	switch {
	case errors.Is(err, storage.ErrTooLarge):
		return problem(ctx, http.StatusRequestEntityTooLarge, "file is too large")
	case errors.Is(err, storage.ErrTypeNotAllowed):
		return problem(ctx, http.StatusUnsupportedMediaType, "this kind of file is not accepted")
	case errors.Is(err, storage.ErrNotMultipart), errors.Is(err, storage.ErrNoFile),
		errors.Is(err, storage.ErrMultipleFiles), errors.Is(err, storage.ErrTooManyParts):
		return problem(ctx, http.StatusBadRequest, "send one file as multipart form data in the \"file\" field")
	case err != nil:
		return err
	}

	doc := Document{
		OwnerID: owner, Key: file.Key, Name: file.Filename, ContentType: file.ContentType,
		Size: file.Size, SHA256: file.SHA256, CreatedAt: time.Now().UTC(),
	}
	err = h.store.InTx(ctx.Context(), func(tx *db.Store) error {
		return tx.Create(ctx.Context(), h.meta, &doc)
	})
	if err != nil {
		h.discard(ctx, file.Key)
		return err
	}
	return ctx.JSON(http.StatusCreated, viewOf(doc))
}

// download serves a document to its owner. Anyone else, and any id that
// does not exist, gets the same 404.
func (h *handlers) download(ctx *tango.Context) error {
	doc, ok, err := h.owned(ctx)
	if err != nil || !ok {
		return h.refuse(ctx, err)
	}
	opts := storage.ServeOptions{Filename: doc.Name}
	if ctx.Query("inline") == "1" {
		opts.InlineTypes = h.limits.Inline
	}
	return storage.Serve(ctx.ResponseWriter(), ctx.Request(), h.objects, doc.Key, opts)
}

// remove deletes the row first, then the bytes on a best-effort basis: a
// failed byte deletion leaves an orphan object nobody can reach, never a
// row pointing at a missing object.
func (h *handlers) remove(ctx *tango.Context) error {
	doc, ok, err := h.owned(ctx)
	if err != nil || !ok {
		return h.refuse(ctx, err)
	}
	if err := h.store.Delete(ctx.Context(), h.meta, doc.ID); err != nil {
		return err
	}
	h.discard(ctx, doc.Key)
	ctx.ResponseWriter().WriteHeader(http.StatusNoContent)
	return nil
}

// discard deletes an object whose row is gone or was never written. The
// request may already be cancelled, so it does not use the request's
// context. A failure is logged: it leaves an unreachable object, which is
// the host's to clean up.
func (h *handlers) discard(ctx *tango.Context, key string) {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx.Context()), 10*time.Second)
	defer cancel()
	if err := h.objects.Delete(cleanup, key); err != nil {
		ctx.Logger().Warn("orphaned object", "err", err)
	}
}

// owned loads the document named by the URL, only if the caller owns it.
func (h *handlers) owned(ctx *tango.Context) (Document, bool, error) {
	owner, ok, err := currentAccountID(ctx, h.store)
	if err != nil || !ok {
		return Document{}, false, err
	}
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		return Document{}, false, errNotFound
	}
	var doc Document
	err = h.store.Get(ctx.Context(), h.meta, id, &doc)
	if errors.Is(err, db.ErrNotFound) || (err == nil && doc.OwnerID != owner) {
		return Document{}, false, errNotFound
	}
	return doc, err == nil, err
}

var errNotFound = errors.New("documents: not found")

// refuse answers a request that cannot proceed: 404 for a missing or
// someone else's document, 401 for no session, and any other error is
// returned for the framework's generic 500.
func (h *handlers) refuse(ctx *tango.Context, err error) error {
	switch {
	case errors.Is(err, errNotFound):
		return problem(ctx, http.StatusNotFound, "not found")
	case err != nil:
		return err
	}
	return problem(ctx, http.StatusUnauthorized, "log in first")
}

func problem(ctx *tango.Context, status int, message string) error {
	return ctx.JSON(status, map[string]string{"error": message})
}

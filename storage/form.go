package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
)

// FormOptions are the host's rules for reading one multipart form.
type FormOptions struct {
	// Files declares the file fields the form may carry, each with the rules
	// its own Put applies.
	Files map[string]PutOptions
	// TokenField, when set, names a field that must be the first part of the
	// body, and CheckToken must accept its value before any other part is
	// read. A forged request then stores nothing. The token is not returned
	// in Form.Values.
	TokenField string
	// CheckToken reports whether the first part's value is valid. It is
	// required when TokenField is set.
	CheckToken func(value string) bool
}

var (
	// ErrFormToken is returned when the form's first part is not the token
	// field or its value is refused. Nothing is stored.
	ErrFormToken = errors.New("storage: form token is missing, late or wrong")
	// ErrUnexpectedFile is returned for a file in a field Files does not
	// declare. Nothing is left stored.
	ErrUnexpectedFile = errors.New("storage: form has a file in an undeclared field")
	// ErrFieldTooLarge is returned for a text field, or a form's text fields
	// together, past their bounds.
	ErrFieldTooLarge = errors.New("storage: form text is too large")
)

const (
	// maxFormParts bounds the parts of one form, files and fields together. It
	// is larger than Upload's bound because a model form has many fields.
	maxFormParts = 256
	// maxFormTextBytes bounds every text field of one form together.
	maxFormTextBytes = 1 << 20
)

// Form is what ReadForm read: the text fields and the files it stored.
type Form struct {
	// Values are the form's text fields. A file field's name is never here.
	Values url.Values
	// Files are the files stored, keyed by field. A field where no file was
	// chosen has no entry.
	Files map[string]UploadedFile
}

// ReadForm reads a multipart/form-data request that holds text fields and
// files, and stores each declared file with store.Put under that field's own
// rules. It streams the files: nothing is buffered in memory.
//
// When opts.TokenField is set, the first part must be that field with a value
// CheckToken accepts, and it is verified before any other part is read, so a
// forged request stores nothing. A field where no file was chosen is not an
// error and has no entry in Form.Files. Any failure after a file was stored
// deletes every object this call stored, even when the request's context is
// done. A body cut off by a body limit surfaces as ErrTooLarge. ReadForm
// writes no response and logs nothing; the calling View maps the errors.
func ReadForm(store Store, r *http.Request, opts FormOptions) (Form, error) {
	if opts.TokenField != "" && opts.CheckToken == nil {
		return Form{}, fmt.Errorf("%w: TokenField needs CheckToken", ErrInvalidOptions)
	}
	reader, err := r.MultipartReader()
	if err != nil {
		if errors.Is(err, http.ErrNotMultipart) || errors.Is(err, http.ErrMissingBoundary) {
			return Form{}, ErrNotMultipart
		}
		return Form{}, mapBodyError(err)
	}
	parts := 0
	if opts.TokenField != "" {
		if err := checkFormToken(reader, opts); err != nil {
			return Form{}, err
		}
		parts++
	}
	form := Form{Values: url.Values{}, Files: map[string]UploadedFile{}}
	textBytes := 0
	fail := func(err error) (Form, error) {
		for _, file := range form.Files {
			_ = store.Delete(context.WithoutCancel(r.Context()), file.Key)
		}
		return Form{}, err
	}
	for ; ; parts++ {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			return form, nil
		}
		if err != nil {
			return fail(mapBodyError(err))
		}
		if parts >= maxFormParts {
			return fail(ErrTooManyParts)
		}
		name := part.FormName()
		rules, declared := opts.Files[name]
		if part.FileName() == "" {
			if declared {
				continue // an empty file input: no file was chosen
			}
			value, err := io.ReadAll(io.LimitReader(part, maxFieldBytes+1))
			if err != nil {
				return fail(mapBodyError(err))
			}
			if textBytes += len(value); len(value) > maxFieldBytes || textBytes > maxFormTextBytes {
				return fail(ErrFieldTooLarge)
			}
			form.Values.Add(name, string(value))
			continue
		}
		if !declared {
			return fail(ErrUnexpectedFile)
		}
		if _, again := form.Files[name]; again {
			return fail(ErrMultipleFiles)
		}
		object, err := store.Put(r.Context(), part, rules)
		if err != nil {
			return fail(mapBodyError(err))
		}
		form.Files[name] = UploadedFile{
			Object:       object,
			Filename:     displayName(part.FileName()),
			DeclaredType: part.Header.Get("Content-Type"),
		}
	}
}

// checkFormToken reads the first part and verifies it is the token field with
// an accepted value. A file part is never read here.
func checkFormToken(reader *multipart.Reader, opts FormOptions) error {
	part, err := reader.NextPart()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return ErrFormToken
		}
		return mapBodyError(err)
	}
	if part.FormName() != opts.TokenField || part.FileName() != "" {
		return ErrFormToken
	}
	value, err := io.ReadAll(io.LimitReader(part, maxFieldBytes))
	if err != nil {
		return mapBodyError(err)
	}
	if !opts.CheckToken(string(value)) {
		return ErrFormToken
	}
	return nil
}

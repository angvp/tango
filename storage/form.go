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
// forged request stores nothing. The token is read once: the same name in a
// later part is refused. A field where no file was chosen is not an error and
// has no entry in Form.Files; a part with no filename that carries bytes in a
// file field is ErrUnexpectedFile. A second file in one field is
// ErrMultipleFiles. Any failure after a file was stored deletes every object
// this call stored, even when the request's context is done. A body cut off
// by a body limit surfaces as ErrTooLarge. ReadForm writes no response and
// logs nothing; the calling View maps the errors.
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
	fr := &formReader{
		store: store, request: r, opts: opts,
		form: Form{Values: url.Values{}, Files: map[string]UploadedFile{}},
	}
	if opts.TokenField != "" {
		if err := checkFormToken(reader, opts); err != nil {
			return Form{}, err
		}
		fr.parts++ // the token was the first part
	}
	if err := fr.readParts(reader); err != nil {
		fr.rollBack()
		return Form{}, err
	}
	return fr.form, nil
}

// formReader holds what one ReadForm call has read so far.
type formReader struct {
	store     Store
	request   *http.Request
	opts      FormOptions
	form      Form
	parts     int
	textBytes int
}

// rollBack deletes every object this call stored. It outlives a canceled
// request, which is often why the call failed.
func (fr *formReader) rollBack() {
	for _, file := range fr.form.Files {
		_ = fr.store.Delete(context.WithoutCancel(fr.request.Context()), file.Key)
	}
}

func (fr *formReader) readParts(reader *multipart.Reader) error {
	for ; ; fr.parts++ {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return mapBodyError(err)
		}
		if fr.parts >= maxFormParts {
			return ErrTooManyParts
		}
		if fr.opts.TokenField != "" && part.FormName() == fr.opts.TokenField {
			return ErrFormToken
		}
		if part.FileName() == "" {
			err = fr.readText(part)
		} else {
			err = fr.storeFile(part)
		}
		if err != nil {
			return err
		}
	}
}

// readText reads a part with no filename: a text field, or the empty input a
// browser sends for a file field where nothing was chosen.
func (fr *formReader) readText(part *multipart.Part) error {
	// One byte past the bound tells a field at the bound from one beyond it.
	value, err := io.ReadAll(io.LimitReader(part, maxFieldBytes+1))
	if err != nil {
		return mapBodyError(err)
	}
	if _, isFile := fr.opts.Files[part.FormName()]; isFile {
		if len(value) > 0 {
			return ErrUnexpectedFile
		}
		return nil
	}
	if fr.textBytes += len(value); len(value) > maxFieldBytes || fr.textBytes > maxFormTextBytes {
		return ErrFieldTooLarge
	}
	fr.form.Values.Add(part.FormName(), string(value))
	return nil
}

// storeFile stores a part that carries a filename, under its field's rules.
func (fr *formReader) storeFile(part *multipart.Part) error {
	name := part.FormName()
	rules, declared := fr.opts.Files[name]
	if !declared {
		return ErrUnexpectedFile
	}
	if _, again := fr.form.Files[name]; again {
		return ErrMultipleFiles
	}
	object, err := fr.store.Put(fr.request.Context(), part, rules)
	if err != nil {
		return mapBodyError(err)
	}
	fr.form.Files[name] = UploadedFile{
		Object:       object,
		Filename:     displayName(part.FileName()),
		DeclaredType: part.Header.Get("Content-Type"),
	}
	return nil
}

// checkFormToken reads the first part and verifies it is the token field with
// an accepted value. A file part is never read here, and a token past the
// field bound is refused, not truncated and compared.
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
	value, err := io.ReadAll(io.LimitReader(part, maxFieldBytes+1))
	if err != nil {
		return mapBodyError(err)
	}
	if len(value) > maxFieldBytes || !opts.CheckToken(string(value)) {
		return ErrFormToken
	}
	return nil
}

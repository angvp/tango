package storage

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// DefaultUploadField is the form field Upload reads when
	// UploadOptions.Field is empty.
	DefaultUploadField = "file"

	// maxUploadParts bounds the parts of one request, files and fields
	// together.
	maxUploadParts = 32
	// maxFieldBytes bounds how much of an ordinary form field Upload reads
	// before skipping the rest.
	maxFieldBytes = 64 << 10
	// maxFilenameBytes bounds the display name Upload returns.
	maxFilenameBytes = 255
)

var (
	// ErrNotMultipart is returned for a request that is not
	// multipart/form-data.
	ErrNotMultipart = errors.New("storage: request is not multipart/form-data")
	// ErrNoFile is returned when the request carries no file in the field
	// Upload reads.
	ErrNoFile = errors.New("storage: request has no file in the upload field")
	// ErrMultipleFiles is returned when the request carries more than one
	// file part. Nothing is left stored.
	ErrMultipleFiles = errors.New("storage: request has more than one file")
	// ErrTooManyParts is returned when the request has more parts than
	// Upload reads. Nothing is left stored.
	ErrTooManyParts = errors.New("storage: request has too many parts")
)

// UploadOptions are the host's rules for reading one upload.
type UploadOptions struct {
	// Field is the form field holding the file. It defaults to "file".
	Field string
	// PutOptions are passed to Store.Put unchanged, so the size cap and the
	// allowed-types policy apply exactly as they do to a direct Put.
	PutOptions
}

// UploadedFile is what Upload stored: the Object, plus what the client said
// about it. Filename and DeclaredType are host metadata only. They are never
// used as a key, a path, a validation input or an authorization decision.
type UploadedFile struct {
	Object
	// Filename is the client's filename reduced to a safe display name: no
	// directories, no control or format characters, valid UTF-8, at most 255 bytes.
	// It may be empty.
	Filename string
	// DeclaredType is the Content-Type the client sent for the part. It is
	// not the stored type; Object.ContentType is the sniffed one.
	DeclaredType string
}

// Upload reads the one file in a multipart/form-data request and stores it
// with store.Put. It streams the file: nothing is buffered in memory, and a
// file past opts.MaxSize is refused with ErrTooLarge as soon as the cap is
// passed, without reading the rest. Other form fields are read, bounded, and
// ignored.
//
// The request must hold exactly one file part, within a bounded number of
// parts; otherwise Upload fails with ErrNoFile, ErrMultipleFiles,
// ErrTooManyParts or ErrNotMultipart and leaves nothing stored. A request
// body cut off by a body limit such as tango.MaxBodySize surfaces as
// ErrTooLarge too. Map these to statuses in the calling View; Upload
// writes no response and logs nothing.
func Upload(store Store, r *http.Request, opts UploadOptions) (UploadedFile, error) {
	field := opts.Field
	if field == "" {
		field = DefaultUploadField
	}
	reader, err := r.MultipartReader()
	if err != nil {
		if errors.Is(err, http.ErrNotMultipart) || errors.Is(err, http.ErrMissingBoundary) {
			return UploadedFile{}, ErrNotMultipart
		}
		return UploadedFile{}, mapBodyError(err)
	}

	var (
		stored    UploadedFile
		haveFile  bool
		fileParts int
	)
	// discard removes a stored file when the request turns out to be invalid.
	discard := func(err error) (UploadedFile, error) {
		if haveFile {
			_ = store.Delete(context.WithoutCancel(r.Context()), stored.Key)
		}
		return UploadedFile{}, err
	}
	for parts := 0; ; parts++ {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return discard(mapBodyError(err))
		}
		if parts >= maxUploadParts {
			return discard(ErrTooManyParts)
		}
		if part.FileName() == "" {
			if _, err := io.Copy(io.Discard, io.LimitReader(part, maxFieldBytes)); err != nil {
				return discard(mapBodyError(err))
			}
			continue
		}
		fileParts++
		if fileParts > 1 {
			return discard(ErrMultipleFiles)
		}
		if part.FormName() != field {
			continue
		}
		object, err := store.Put(r.Context(), part, opts.PutOptions)
		if err != nil {
			return UploadedFile{}, mapBodyError(err)
		}
		stored = UploadedFile{
			Object:       object,
			Filename:     displayName(part.FileName()),
			DeclaredType: part.Header.Get("Content-Type"),
		}
		haveFile = true
	}
	if !haveFile {
		return UploadedFile{}, ErrNoFile
	}
	return stored, nil
}

// mapBodyError turns a body limit's rejection into ErrTooLarge and leaves
// every other error alone.
func mapBodyError(err error) error {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return &TooLargeError{Max: tooLarge.Limit}
	}
	return err
}

// displayName reduces a client filename to something safe to show: its last
// path element (either separator), without control or format characters, as valid
// UTF-8, within maxFilenameBytes.
func displayName(name string) string {
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.ToValidUTF8(name, "")
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, name)
	if name == "." || name == ".." {
		return ""
	}
	for len(name) > maxFilenameBytes {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-size]
	}
	return name
}

package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"mime"
	"net/http"
	"strings"
)

// sniffLength is how many bytes content sniffing looks at.
const sniffLength = 512

// Inspector wraps the reader of a write. Every Store uses it, so size, type
// and hash behave the same everywhere. It sniffs the content type from the
// first bytes, enforces opts, and hashes and counts what passes through.
type Inspector struct {
	head        []byte
	src         io.Reader
	max         int64
	size        int64
	sum         hash.Hash
	contentType string
	err         error
}

// Inspect reads the start of r, sniffs its content type, and refuses it with
// ErrTypeNotAllowed before any more is read. Read the returned Inspector to
// the end, then call Object. Reading past opts.MaxSize fails with
// ErrTooLarge.
func Inspect(r io.Reader, opts PutOptions) (*Inspector, error) {
	if opts.MaxSize <= 0 {
		return nil, ErrInvalidOptions
	}
	head := make([]byte, sniffLength)
	n, err := io.ReadFull(r, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, err
	}
	head = head[:n]
	if int64(n) > opts.MaxSize {
		return nil, &TooLargeError{Max: opts.MaxSize}
	}
	contentType := http.DetectContentType(head)
	if !typeAllowed(contentType, opts.AllowedTypes) {
		return nil, &TypeNotAllowedError{Type: contentType}
	}
	i := &Inspector{head: head, src: r, max: opts.MaxSize, sum: sha256.New(), contentType: contentType}
	if n < sniffLength {
		i.src = strings.NewReader("") // r is exhausted
	}
	return i, nil
}

// Read implements io.Reader.
func (i *Inspector) Read(p []byte) (int, error) {
	if i.err != nil {
		return 0, i.err
	}
	var n int
	var err error
	if len(i.head) > 0 {
		n = copy(p, i.head)
		i.head = i.head[n:]
	} else {
		n, err = i.src.Read(p)
	}
	i.size += int64(n)
	if i.size > i.max {
		i.err = &TooLargeError{Max: i.max}
		return 0, i.err
	}
	i.sum.Write(p[:n])
	if err != nil && err != io.EOF {
		i.err = err
	}
	return n, err
}

// Object returns what was read, under key. Call it after Read returned
// io.EOF.
func (i *Inspector) Object(key string) Object {
	return Object{Key: key, Size: i.size, SHA256: hex.EncodeToString(i.sum.Sum(nil)), ContentType: i.contentType}
}

// typeAllowed reports whether the sniffed contentType is in allowed; an
// empty allowed list accepts everything.
func typeAllowed(contentType string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	media, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	for _, pattern := range allowed {
		pattern = strings.ToLower(strings.TrimSpace(pattern))
		if prefix, ok := strings.CutSuffix(pattern, "/*"); ok {
			if strings.HasPrefix(media, prefix+"/") {
				return true
			}
		} else if media == pattern {
			return true
		}
	}
	return false
}

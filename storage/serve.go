package storage

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// defaultCacheControl keeps an authorized download out of shared caches.
const defaultCacheControl = "private, no-store"

// ServeOptions shape one response from Serve.
type ServeOptions struct {
	// Filename is the name offered to the browser in Content-Disposition. It
	// comes from the host (its metadata), never from the stored object. It
	// is reduced to a base name, stripped of control characters, and encoded
	// per RFC 6266 and RFC 5987; if nothing usable is left no name is sent.
	Filename string
	// InlineTypes lists the stored media types the browser may show inline,
	// such as "image/png" or "image/*". Every other type is sent as an
	// attachment. Active content (HTML, XHTML, SVG, XML, JavaScript, CSS) is
	// always an attachment, even when listed, so a stored file cannot run
	// in the application's origin.
	InlineTypes []string
	// CacheControl replaces the default "private, no-store".
	CacheControl string
}

// Serve writes the object at key to w for a GET or HEAD request. The host
// calls it only after it has authorized the request: Serve checks nothing
// about who is asking, and never uses a key, a filename or a client-declared
// type as an access decision.
//
// It supports a single Range, If-None-Match, If-Modified-Since and
// If-Range; several ranges, or a range it cannot parse, get the whole
// object. The Content-Type is the type sniffed when the object was stored,
// and every response carries X-Content-Type-Options: nosniff. A missing
// object and an invalid key give the same 404.
//
// Serve returns nil whenever it answered, and returns any unexpected store
// error without writing a response, so the host's View can return it and
// let the framework answer 500 and log it. Serve itself never logs.
func Serve(w http.ResponseWriter, r *http.Request, store Store, key string, opts ServeOptions) error {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return nil
	}
	info, err := store.Stat(r.Context(), key)
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrInvalidKey) {
		writeError(w, http.StatusNotFound, "not found")
		return nil
	}
	if err != nil {
		return err
	}

	setObjectHeaders(w.Header(), info, opts)
	if notModified(r, info) {
		w.WriteHeader(http.StatusNotModified)
		return nil
	}

	start, length, status := int64(0), info.Size, http.StatusOK
	if spec := r.Header.Get("Range"); spec != "" && ifRangeHolds(r, info) {
		switch s, n, result := parseRange(spec, info.Size); result {
		case rangeSatisfiable:
			start, length, status = s, n, http.StatusPartialContent
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", s, s+n-1, info.Size))
		case rangeUnsatisfiable:
			w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", info.Size))
			w.Header().Set("Content-Length", "0")
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return nil
		}
	}
	w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
	if r.Method == http.MethodHead {
		w.WriteHeader(status)
		return nil
	}
	body, err := store.Open(r.Context(), key, start, length)
	if errors.Is(err, ErrNotFound) {
		// Deleted between Stat and Open.
		w.Header().Del("Content-Length")
		writeError(w, http.StatusNotFound, "not found")
		return nil
	}
	if err != nil {
		return err
	}
	defer body.Close()
	w.WriteHeader(status)
	_, err = io.Copy(w, body)
	return err
}

// setObjectHeaders sets the headers every successful response shares.
func setObjectHeaders(h http.Header, info Info, opts ServeOptions) {
	contentType := info.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	h.Set("Content-Type", contentType)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Accept-Ranges", "bytes")
	h.Set("ETag", info.ETag)
	h.Set("Last-Modified", info.ModTime.UTC().Format(http.TimeFormat))
	h.Set("Content-Disposition", disposition(contentType, opts))
	cacheControl := opts.CacheControl
	if cacheControl == "" {
		cacheControl = defaultCacheControl
	}
	h.Set("Cache-Control", cacheControl)
}

// writeError answers with the framework's JSON error shape, which no client
// can tell apart for a missing object and an invalid key.
func writeError(w http.ResponseWriter, status int, message string) {
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, `{"error":"`+message+`"}`+"\n")
}

// notModified applies If-None-Match, or If-Modified-Since when there is no
// If-None-Match.
func notModified(r *http.Request, info Info) bool {
	if header := r.Header.Get("If-None-Match"); header != "" {
		return etagListMatches(header, info.ETag)
	}
	since, err := http.ParseTime(r.Header.Get("If-Modified-Since"))
	if err != nil {
		return false
	}
	return !info.ModTime.Truncate(time.Second).After(since)
}

// etagListMatches reports whether header, a list of entity tags or "*",
// contains etag, comparing weakly.
func etagListMatches(header, etag string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimPrefix(strings.TrimSpace(candidate), "W/")
		if candidate == "*" || candidate == etag {
			return true
		}
	}
	return false
}

// ifRangeHolds reports whether a Range may be honoured: there is no If-Range,
// or it names the current ETag or modification time.
func ifRangeHolds(r *http.Request, info Info) bool {
	condition := r.Header.Get("If-Range")
	if condition == "" {
		return true
	}
	if strings.HasPrefix(condition, `"`) {
		return condition == info.ETag
	}
	when, err := http.ParseTime(condition)
	return err == nil && when.Equal(info.ModTime.Truncate(time.Second))
}

type rangeResult int

const (
	rangeIgnored rangeResult = iota
	rangeSatisfiable
	rangeUnsatisfiable
)

// parseRange reads a single "bytes=" range against size. Several ranges, a
// unit other than bytes, or a malformed spec are ignored (the whole object is
// sent); a well-formed range outside the object is unsatisfiable.
func parseRange(header string, size int64) (start, length int64, result rangeResult) {
	spec, ok := strings.CutPrefix(header, "bytes=")
	if !ok || strings.Contains(spec, ",") {
		return 0, 0, rangeIgnored
	}
	first, last, ok := strings.Cut(strings.TrimSpace(spec), "-")
	if !ok {
		return 0, 0, rangeIgnored
	}
	if first == "" { // a suffix: the last n bytes
		n, ok := parseDigits(last)
		if !ok {
			return 0, 0, rangeIgnored
		}
		if n == 0 || size == 0 {
			return 0, 0, rangeUnsatisfiable
		}
		n = min(n, size)
		return size - n, n, rangeSatisfiable
	}
	from, ok := parseDigits(first)
	if !ok {
		return 0, 0, rangeIgnored
	}
	to := size - 1
	if last != "" {
		if to, ok = parseDigits(last); !ok || to < from {
			return 0, 0, rangeIgnored
		}
	}
	if from >= size {
		return 0, 0, rangeUnsatisfiable
	}
	to = min(to, size-1)
	return from, to - from + 1, rangeSatisfiable
}

// parseDigits parses a non-negative decimal number made only of digits.
func parseDigits(s string) (int64, bool) {
	if s == "" || strings.Trim(s, "0123456789") != "" {
		return 0, false
	}
	n, err := strconv.ParseInt(s, 10, 64)
	return n, err == nil
}

// disposition builds Content-Disposition: inline only for a listed type that
// is not active content, otherwise attachment.
func disposition(contentType string, opts ServeOptions) string {
	kind := "attachment"
	media, _, err := mime.ParseMediaType(contentType)
	if err == nil && len(opts.InlineTypes) > 0 && !activeContent(media) && typeAllowed(contentType, opts.InlineTypes) {
		kind = "inline"
	}
	name := cleanFilename(opts.Filename)
	if name == "" {
		return kind
	}
	value := kind + `; filename="` + asciiFallback(name) + `"`
	if !isASCII(name) {
		value += "; filename*=UTF-8''" + percentEncode(name)
	}
	return value
}

// activeContent reports whether a browser may execute or interpret a media
// type of this kind in the page's origin.
func activeContent(media string) bool {
	switch media {
	case "text/html", "application/xhtml+xml", "image/svg+xml", "text/xml", "application/xml", "text/css":
		return true
	}
	return strings.HasSuffix(media, "+xml") || strings.Contains(media, "javascript") || strings.Contains(media, "ecmascript")
}

// maxFilenameRunes bounds the name sent in a header.
const maxFilenameRunes = 200

// cleanFilename reduces name to a safe base name: no directories, no control
// characters, at most maxFilenameRunes runes, and nothing for "." or "..".
func cleanFilename(name string) string {
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name)
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.TrimSpace(name)
	if name == "." || name == ".." {
		return ""
	}
	if utf8.RuneCountInString(name) > maxFilenameRunes {
		name = string([]rune(name)[:maxFilenameRunes])
	}
	return name
}

// asciiFallback replaces everything but a conservative set of characters
// with an underscore, for clients that ignore filename*.
func asciiFallback(name string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		case strings.ContainsRune(" ._-+()[],=@~", r):
			return r
		}
		return '_'
	}, name)
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// percentEncode encodes s as RFC 5987 ext-value characters.
func percentEncode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("!#$&+-.^_`|~", c) >= 0 {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

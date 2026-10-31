package accounts

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"net/http"
	"path"
	"strings"

	"github.com/angvp/tango"
)

// assetRoutePrefix is where accounts serves its own static files. Each URL
// carries a hash of the file's exact bytes, so the response can be cached
// immutably and a cached copy can never be stale.
const assetRoutePrefix = "/accounts/_assets/"

//go:embed assets/accounts.css
var assetFiles embed.FS

// staticAsset is one embedded accounts file served at its content-hashed URL.
type staticAsset struct {
	URL         string
	ContentType string
	Body        []byte
}

// newStaticAsset embeds source and names it name.<hash><ext>, where hash is
// the first 16 hex digits of the SHA-256 of its bytes.
func newStaticAsset(source, contentType string) staticAsset {
	body, err := assetFiles.ReadFile(source)
	if err != nil {
		panic(fmt.Sprintf("accounts: embedded asset %s: %v", source, err))
	}
	sum := sha256.Sum256(body)
	ext := path.Ext(source)
	name := strings.TrimSuffix(path.Base(source), ext)
	return staticAsset{
		URL:         assetRoutePrefix + name + "." + hex.EncodeToString(sum[:8]) + ext,
		ContentType: contentType,
		Body:        body,
	}
}

// accountsStylesheet styles the error paragraph of the account pages. The
// pages reference it instead of carrying an inline style, so a strict
// Content-Security-Policy keeps the error's colour.
var accountsStylesheet = newStaticAsset("assets/accounts.css", "text/css; charset=utf-8")

// stylesheetLinkTag links the accounts stylesheet from every account page.
var stylesheetLinkTag = `<link rel="stylesheet" href="` + accountsStylesheet.URL + `">`

// assetRoute serves the accounts stylesheet at its hashed URL. It is public,
// since it contains no data.
func assetRoute() tango.Route {
	asset := accountsStylesheet
	return tango.Path(http.MethodGet, asset.URL, func(ctx *tango.Context) error {
		w := ctx.ResponseWriter()
		w.Header().Set("Content-Type", asset.ContentType)
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, err := w.Write(asset.Body)
		return err
	}, hiddenPage)
}

package admin

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/angvp/tango"
	"github.com/angvp/tango/i18n"
	"github.com/angvp/tango/internal/security"
)

// flashCookieName carries one outcome across a redirect to the next page.
const flashCookieName = "tango_admin_flash"

// flashCookieMaxAge bounds how long an unread outcome waits in the browser.
const flashCookieMaxAge = 300

// flashMaxLength rejects an oversized cookie before anything is decoded.
const flashMaxLength = 1024

// flashKeyLabel domain-separates the flash key from the CSRF derivation,
// which hashes the session token without a key.
const flashKeyLabel = "tango admin flash v1"

// flashOutcome is one catalogued outcome: the English fallback, the number of
// arguments it takes, and whether the first argument is a count. A count is
// sent as a decimal string and rendered as a number.
type flashOutcome struct {
	fallback string
	args     int
	counted  bool
}

// flashOutcomes is the complete set of outcomes the transport renders. A key
// outside this set is ignored, so a signed cookie cannot show arbitrary text.
var flashOutcomes = map[string]flashOutcome{
	"admin.outcome.created":   {fallback: "The %s was added.", args: 1},
	"admin.outcome.saved":     {fallback: "The %s was saved.", args: 1},
	"admin.outcome.deleted":   {fallback: "The %s was deleted.", args: 1},
	"admin.bulk.outcome_one":  {fallback: "1 %s deleted", args: 1},
	"admin.bulk.outcome_many": {fallback: "%d %s deleted", args: 2, counted: true},
}

// flashPayload is the signed content of a flash cookie.
type flashPayload struct {
	Key  string   `json:"k"`
	Args []string `json:"a"`
}

// flashKey derives the MAC key from the session token. The session token is
// the only per-session secret the admin holds, so a value minted for one
// session fails verification under every other session's token.
func flashKey(sessionToken string) []byte {
	derive := hmac.New(sha256.New, []byte(sessionToken))
	derive.Write([]byte(flashKeyLabel))
	return derive.Sum(nil)
}

func flashMAC(sessionToken string, payload []byte) []byte {
	mac := hmac.New(sha256.New, flashKey(sessionToken))
	mac.Write(payload)
	return mac.Sum(nil)
}

// sealFlash encodes a key and its arguments as a flash cookie value signed
// for sessionToken. It does not check the key; setFlash does.
func sealFlash(sessionToken, key string, args ...string) (string, error) {
	raw, err := json.Marshal(flashPayload{Key: key, Args: args})
	if err != nil {
		return "", err
	}
	encoding := base64.RawURLEncoding
	return encoding.EncodeToString(raw) + "." + encoding.EncodeToString(flashMAC(sessionToken, raw)), nil
}

// openFlash returns the outcome key and arguments a cookie value carries for
// sessionToken. ok is false for a value that is missing, oversized, malformed,
// signed for another session, altered, or naming an outcome that is not
// catalogued or takes other arguments.
func openFlash(sessionToken, value string) (key string, args []string, ok bool) {
	if sessionToken == "" || value == "" || len(value) > flashMaxLength {
		return "", nil, false
	}
	encodedPayload, encodedMAC, found := strings.Cut(value, ".")
	if !found {
		return "", nil, false
	}
	encoding := base64.RawURLEncoding
	raw, err := encoding.DecodeString(encodedPayload)
	if err != nil {
		return "", nil, false
	}
	mac, err := encoding.DecodeString(encodedMAC)
	if err != nil || !hmac.Equal(mac, flashMAC(sessionToken, raw)) {
		return "", nil, false
	}
	var payload flashPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", nil, false
	}
	outcome, known := flashOutcomes[payload.Key]
	if !known || len(payload.Args) != outcome.args {
		return "", nil, false
	}
	if outcome.counted {
		if count, err := strconv.Atoi(payload.Args[0]); err != nil || count < 1 || strconv.Itoa(count) != payload.Args[0] {
			return "", nil, false
		}
	}
	return payload.Key, payload.Args, true
}

// setFlash stores an outcome for the session about to be redirected. The key
// must be catalogued and take exactly the given arguments.
func setFlash(w http.ResponseWriter, r *http.Request, sessionToken, key string, args ...string) error {
	outcome, known := flashOutcomes[key]
	if !known || len(args) != outcome.args {
		return errors.New("tango admin: outcome is not catalogued with these arguments")
	}
	if sessionToken == "" {
		return errors.New("tango admin: outcome has no session to bind to")
	}
	value, err := sealFlash(sessionToken, key, args...)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     flashCookieName,
		Value:    value,
		Path:     adminRootPath,
		MaxAge:   flashCookieMaxAge,
		HttpOnly: true,
		Secure:   security.IsHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

// clearFlashCookie expires the flash cookie.
func clearFlashCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     flashCookieName,
		Value:    "",
		Path:     adminRootPath,
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   security.IsHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	})
}

// takeFlash expires the flash cookie and returns the outcome text it carried
// for sessionToken, or "" when it carried none that is genuine. The cookie
// is expired whether or not its value verifies.
func takeFlash(ctx context.Context, w http.ResponseWriter, r *http.Request, sessionToken string) string {
	cookie, err := r.Cookie(flashCookieName)
	if err != nil {
		return ""
	}
	clearFlashCookie(w, r)
	key, args, ok := openFlash(sessionToken, cookie.Value)
	if !ok {
		return ""
	}
	return flashText(ctx, key, args)
}

// flashText renders a catalogued outcome through the i18n catalog, with its
// English fallback.
func flashText(ctx context.Context, key string, args []string) string {
	outcome := flashOutcomes[key]
	values := make([]any, len(args))
	for i, arg := range args {
		values[i] = arg
	}
	if outcome.counted {
		count, _ := strconv.Atoi(args[0])
		values[0] = count
	}
	return i18n.T(ctx, key, outcome.fallback, values...)
}

// sessionTokenOf returns the session token a request carries, or "" when
// none is present. requireSession guarantees one for every view that writes
// an outcome.
func sessionTokenOf(r *http.Request) string {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

// statusNotice is the outcome the status region shows, with the form that
// dismisses it.
type statusNotice struct {
	Text      string
	Dismiss   string
	Next      string
	CSRFToken string
}

// dismissView clears the status region for the page the form was sent from.
// The outcome was already expired when it was shown, so the response only
// makes the dismissal explicit and returns the person to the same page.
func dismissView() tango.View {
	return func(ctx *tango.Context) error {
		if err := ctx.Request().ParseForm(); err != nil {
			return err
		}
		if !verifySessionCSRF(ctx) {
			return forbiddenCSRF(ctx)
		}
		clearFlashCookie(ctx.ResponseWriter(), ctx.Request())
		return ctx.Redirect(safeAdminNext(ctx.Request().PostFormValue("next"), adminRootPath))
	}
}

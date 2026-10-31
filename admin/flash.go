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

// flashLifetime is how long a sealed outcome verifies after it was issued. It
// matches the cookie's MaxAge, so a copy of the cookie stops verifying when the
// browser would have dropped it.
const flashLifetime = flashCookieMaxAge * time.Second

// flashClockSkew tolerates a server clock that is slightly behind the clock
// that issued the value; the same host issues and verifies, so this is small.
const flashClockSkew = time.Minute

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

// flashPayload is the signed content of a flash cookie. Issued is the Unix
// time the outcome was set, so the server can expire a copy of the cookie.
type flashPayload struct {
	Key    string   `json:"k"`
	Args   []string `json:"a"`
	Issued int64    `json:"t"`
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

// sealFlashAt encodes a key, its arguments and the time it was issued as a
// flash cookie value signed for sessionToken. It does not check the key;
// setFlash does.
func sealFlashAt(sessionToken, key string, issued time.Time, args ...string) (string, error) {
	raw, err := json.Marshal(flashPayload{Key: key, Args: args, Issued: issued.Unix()})
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
	if age := time.Since(time.Unix(payload.Issued, 0)); age > flashLifetime || age < -flashClockSkew {
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

// eventOutcomeNotCarried is logged when a committed write's outcome cannot be
// carried to the next page. The write itself has succeeded and the redirect
// proceeds; only the announcement is lost.
const eventOutcomeNotCarried = "admin outcome not carried"

// errOutcomeTooLong reports a sealed outcome larger than flashMaxLength. The
// decoder would reject such a value, so it is never written.
var errOutcomeTooLong = errors.New("tango admin: outcome is too long to carry in a cookie")

// setOutcome sets the outcome for the request's session before a redirect that
// follows a committed write. A failure is logged with the outcome's key and the
// redirect still proceeds: the write has succeeded, so an error page would
// misreport it.
func setOutcome(ctx *tango.Context, key string, args ...string) {
	if err := setFlash(ctx.ResponseWriter(), ctx.Request(), sessionCookieValue(ctx.Request()), key, args...); err != nil {
		ctx.Logger().Error(eventOutcomeNotCarried, "outcome", key, "error", err)
	}
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
	value, err := sealFlashAt(sessionToken, key, time.Now(), args...)
	if err != nil {
		return err
	}
	if len(value) > flashMaxLength {
		return errOutcomeTooLong
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

// hasFlashCookie reports whether the request carries a flash cookie at all,
// genuine or not; a page that renders expires whichever it finds.
func hasFlashCookie(r *http.Request) bool {
	_, err := r.Cookie(flashCookieName)
	return err == nil
}

// readFlash returns the outcome text the request's flash cookie carries for
// sessionToken, or "" when it carries none that is genuine. It does not expire
// the cookie: that happens when a page that shows the outcome renders.
func readFlash(ctx context.Context, r *http.Request, sessionToken string) string {
	cookie, err := r.Cookie(flashCookieName)
	if err != nil {
		return ""
	}
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

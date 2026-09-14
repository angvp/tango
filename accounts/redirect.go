package accounts

import "github.com/angvp/tango/internal/security"

// safeAccountsNext validates raw as a same-origin redirect target,
// returning fallback otherwise. Unlike admin (whose next only ever needs
// to stay within /admin/), accounts's login can be reached from anywhere
// on the host site, so the allowed prefix is "/" — any same-origin path —
// rather than "/accounts/". This does not reopen an open-redirect hole:
// IsSafeRedirect/SafeRedirect still reject off-site and protocol-relative
// targets regardless of prefix; "/" only stops narrowing same-origin
// destinations that aren't accounts's own concern to restrict.
func safeAccountsNext(raw string, fallback string) string {
	return security.SafeRedirect(raw, "/", fallback)
}

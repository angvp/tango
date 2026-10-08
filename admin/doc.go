// Package admin is tanGO's installable admin site: list, search, create,
// edit and delete pages for every model an application registers with it,
// behind a session-cookie login with CSRF protection and rate-limited
// login attempts.
//
// Install it by adding [New] to a project's InstalledApps. Apps register
// their models through the admin [Registry] they receive, with [Options]
// for list columns, search, ordering and form presentation. Accounts are
// managed from the command line (tango admin create, resetpassword,
// deactivate, grant-staff and the rest), which [HandleCLI] serves, or
// directly with [CreateAccount] and its siblings.
//
// [Widget], the presentation fields of [Options], the built-in widgets and
// [Branding] are best-effort: they may change in any minor release. The
// admin's HTML, templates, CSS and URLs are not covered by the
// compatibility promise either. See
// https://tangoframework.com/docs/guides/admin-registration/.
package admin

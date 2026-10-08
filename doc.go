// Package tango is a Go web framework for database-backed applications,
// inspired by Django's productivity and kept explicit: no code generation
// at runtime, no hidden discovery, plain Go you can read top to bottom.
//
// A project is a list of [App] values in a [Config]. Each App contributes
// its models, routes, admin registrations, checks, jobs and lifecycle
// components to a [Registry] when the project starts. A project's main
// function opens the database (see [LoadDBConfigFromEnv]), lets
// [DispatchFlags] handle the app-side flags the tango CLI relies on
// (-check, -migrate, -tango-dump-models, -tango-status and the admin
// account flags), and otherwise serves HTTP with [ServeContext]. Views are
// plain functions of a [Context].
//
// The packages beside this one provide the rest: [github.com/angvp/tango/db]
// stores models in SQLite or PostgreSQL, [github.com/angvp/tango/migration]
// applies the migrations `tango makemigrations` generates,
// [github.com/angvp/tango/admin] is a ready-made admin site, and
// [github.com/angvp/tango/accounts], [github.com/angvp/tango/auth],
// [github.com/angvp/tango/realtime] and [github.com/angvp/tango/ratelimit]
// cover what most applications need next.
//
// The tutorial and guides are at https://tangoframework.com/docs/. What
// each release promises not to break, and how breaking changes are
// announced, is at https://tangoframework.com/docs/compatibility/.
package tango

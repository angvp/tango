// Package preflight holds the CLI preflight checks that must hold on every
// dialect: -migrate and -check against a conflicting registry leave a fresh
// database untouched, a valid -migrate reaches it, and no flag pair applies a
// write under a conflict. The recording driver is SQLite regardless of the
// run's dialect, so its zero-call assertions behave the same everywhere. It
// lives apart from the root package so CI runs these on PostgreSQL and MySQL.
package preflight

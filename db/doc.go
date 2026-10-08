// Package db stores tanGO models in SQLite or PostgreSQL.
//
// A [Store] wraps a *sql.DB with the [Dialect] it speaks: [NewStore]
// creates one, and its methods create, read, update, delete and filter
// models, generating each dialect's SQL. Raw SQL goes through the Store
// too, when a query outgrows the built-in filtering. [ParseDSN] turns a
// scheme-qualified connection string (sqlite://app.db, postgres://…) into
// the dialect, driver name and connection string to open, so the three
// can't disagree.
//
// See https://tangoframework.com/docs/guides/persistence-crud-and-raw-sql/
// and https://tangoframework.com/docs/guides/sqlite-and-postgresql-setup/.
package db

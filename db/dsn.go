package db

import (
	"errors"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

const sqliteScheme = "sqlite://"

// errUnsupportedDSN never echoes the rejected DSN, which may carry
// credentials.
var errUnsupportedDSN = errors.New("db: unsupported DSN; want sqlite://<path>, sqlite:///<absolute path>, sqlite://:memory:, postgres://… (postgresql://… also works), or mysql://…")

// errInvalidMySQLDSN is the same for a mysql:// DSN that does not parse.
var errInvalidMySQLDSN = errors.New("db: invalid mysql:// DSN; want mysql://user:password@host:port/database[?options]")

// mysqlSQLMode is MySQL 8.4's own default SQL mode, which includes strict
// mode. The driver would otherwise leave whatever the server is configured
// with, and a non-strict server silently truncates over-long values.
const mysqlSQLMode = "ONLY_FULL_GROUP_BY,STRICT_TRANS_TABLES,NO_ZERO_IN_DATE,NO_ZERO_DATE,ERROR_FOR_DIVISION_BY_ZERO,NO_ENGINE_SUBSTITUTION"

// DSN is a parsed scheme-qualified DSN: the Dialect to build a Store
// with, and the two arguments to pass to sql.Open.
type DSN struct {
	// Dialect is the SQL dialect the DSN's scheme names.
	Dialect Dialect
	// Driver is the database/sql driver name: "sqlite", "pgx" or "mysql".
	Driver string
	// Source is the driver's own data source name.
	Source string
}

// ParseDSN turns a scheme-qualified DSN into the Dialect, the database/sql
// driver name, and the driver's own data source name to pass to sql.Open:
//
//	sqlite://app.db            SQLite file relative to the working directory
//	sqlite:///var/data/app.db  SQLite file at an absolute path
//	sqlite://:memory:          SQLite, in-memory
//	postgres://… postgresql://… PostgreSQL, passed to pgx unchanged
//	mysql://user:pass@host:3306/app  MySQL, converted to the driver's own form
//
// Anything else, including a bare path such as "app.db", is an error. For
// SQLite the returned Source already enables foreign key enforcement (see
// SQLiteForeignKeysDSN) and makes a writer wait up to 5 seconds for another
// connection's write lock instead of failing with SQLITE_BUSY, unless the
// DSN sets its own busy_timeout.
//
// For MySQL the returned Source reads and writes times in UTC (parseTime and
// loc=UTC), counts matched rows as affected (clientFoundRows), uses utf8mb4
// with the utf8mb4_bin collation, a strict SQL mode, and the READ COMMITTED
// isolation level. Query options on the mysql:// DSN are the driver's own
// and override those defaults; to choose another isolation level add
// transaction_isolation='REPEATABLE-READ' (quotes percent-encoded as %27).
// A DSN that does not parse is rejected without echoing it.
//
// ParseDSN never imports or registers a driver for the caller: the app still
// imports modernc.org/sqlite ("sqlite"), github.com/jackc/pgx/v5/stdlib
// ("pgx") or github.com/go-sql-driver/mysql ("mysql") itself.
func ParseDSN(dsn string) (DSN, error) {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		return DSN{Dialect: Postgres, Driver: "pgx", Source: dsn}, nil
	}
	if strings.HasPrefix(dsn, "mysql://") {
		return parseMySQLDSN(dsn)
	}
	path, ok := strings.CutPrefix(dsn, sqliteScheme)
	if !ok || path == "" || strings.HasPrefix(path, "?") {
		return DSN{}, errUnsupportedDSN
	}
	return DSN{Dialect: SQLite, Driver: "sqlite", Source: SQLiteForeignKeysDSN(withSQLiteBusyTimeout(path))}, nil
}

func parseMySQLDSN(dsn string) (DSN, error) {
	u, err := url.Parse(dsn)
	if err != nil || u.Host == "" || u.Hostname() == "" {
		return DSN{}, errInvalidMySQLDSN
	}
	database := strings.TrimPrefix(u.Path, "/")
	if database == "" || strings.Contains(database, "/") {
		return DSN{}, errInvalidMySQLDSN
	}
	port := u.Port()
	if port == "" {
		port = "3306"
	}
	cfg := mysql.NewConfig()
	cfg.User = u.User.Username()
	cfg.Passwd, _ = u.User.Password()
	cfg.Net = "tcp"
	cfg.Addr = net.JoinHostPort(u.Hostname(), port)
	cfg.DBName = database
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	cfg.ClientFoundRows = true
	cfg.Collation = "utf8mb4_bin"
	cfg.Params = map[string]string{
		"transaction_isolation": "'READ-COMMITTED'",
		"sql_mode":              "'" + mysqlSQLMode + "'",
	}
	source := cfg.FormatDSN()
	// The driver applies options in order and the last one wins, so the DSN's
	// own options override the defaults above.
	if extra := u.RawQuery; extra != "" {
		source += "&" + extra
	}
	if _, err := mysql.ParseDSN(source); err != nil {
		return DSN{}, errInvalidMySQLDSN
	}
	return DSN{Dialect: MySQL, Driver: "mysql", Source: source}, nil
}

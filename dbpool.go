package tango

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/angvp/tango/db"
)

// defaultDBMaxIdle is how many idle connections ServeContext keeps by
// default. database/sql keeps 2, so a server handling more than 2 requests
// at once closed and reopened a database connection on almost every request.
const defaultDBMaxIdle = 32

// DBPool sets the limits of the database connection pool. A zero field leaves
// that setting as the *sql.DB has it (for database/sql: unlimited open
// connections, 2 idle, no lifetime limits).
type DBPool struct {
	MaxOpen     int           // most open connections; 0 is unlimited
	MaxIdle     int           // most idle connections kept for reuse
	MaxLifetime time.Duration // longest a connection is reused
	MaxIdleTime time.Duration // longest a connection may sit idle
}

func (p DBPool) validate() error {
	switch {
	case p.MaxOpen < 0:
		return fmt.Errorf("tango: DBPool.MaxOpen must not be negative, got %d", p.MaxOpen)
	case p.MaxIdle < 0:
		return fmt.Errorf("tango: DBPool.MaxIdle must not be negative, got %d", p.MaxIdle)
	case p.MaxLifetime < 0:
		return fmt.Errorf("tango: DBPool.MaxLifetime must not be negative, got %s", p.MaxLifetime)
	case p.MaxIdleTime < 0:
		return fmt.Errorf("tango: DBPool.MaxIdleTime must not be negative, got %s", p.MaxIdleTime)
	}
	return nil
}

// WithDBPool sets the database pool limits ServeContext applies to its
// *sql.DB instead of the default. Fields left zero are not touched, so
// WithDBPool(DBPool{}) keeps the database/sql behavior as it is.
//
// Without this option ServeContext raises the idle connection limit to 32
// when the application has not bounded the pool itself with SetMaxOpenConns;
// a pool the application bounded is left alone.
func WithDBPool(pool DBPool) ServeOption {
	return func(c *serveConfig) { c.dbPool, c.dbPoolSet = pool, true }
}

// maxMySQLConnLifetime is how long a MySQL pool reuses a connection. A MySQL
// server (wait_timeout) or a proxy in front of it closes connections that sit
// idle, and a pool that keeps one longer hands out a dead connection.
const maxMySQLConnLifetime = 5 * time.Minute

// defaultDBPool is the pool ServeContext gives a database the application has
// not bounded itself.
func defaultDBPool(dialect db.Dialect) DBPool {
	pool := DBPool{MaxIdle: defaultDBMaxIdle}
	if dialect == db.MySQL {
		pool.MaxLifetime = maxMySQLConnLifetime
	}
	return pool
}

// applyDBPool configures sqlDB's pool from sc: the explicit WithDBPool value,
// else the default for a pool the application has not bounded.
func applyDBPool(sqlDB *sql.DB, sc serveConfig, dialect db.Dialect) {
	if sqlDB == nil {
		return
	}
	pool := sc.dbPool
	if !sc.dbPoolSet {
		if sqlDB.Stats().MaxOpenConnections != 0 {
			return
		}
		pool = defaultDBPool(dialect)
	}
	if pool.MaxOpen > 0 {
		sqlDB.SetMaxOpenConns(pool.MaxOpen)
	}
	if pool.MaxIdle > 0 {
		sqlDB.SetMaxIdleConns(pool.MaxIdle)
	}
	if pool.MaxLifetime > 0 {
		sqlDB.SetConnMaxLifetime(pool.MaxLifetime)
	}
	if pool.MaxIdleTime > 0 {
		sqlDB.SetConnMaxIdleTime(pool.MaxIdleTime)
	}
}

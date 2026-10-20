package tango

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/angvp/tango/db"

	_ "modernc.org/sqlite"
)

// countingConnector counts how many database connections are opened.
type countingConnector struct {
	drv    driver.Driver
	dsn    string
	opened atomic.Int64
}

func (c *countingConnector) Connect(context.Context) (driver.Conn, error) {
	c.opened.Add(1)
	return c.drv.Open(c.dsn)
}

func (c *countingConnector) Driver() driver.Driver { return c.drv }

func openCounted(t *testing.T) (*sql.DB, *countingConnector) {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "pool.db")
	probe, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	connector := &countingConnector{drv: probe.Driver(), dsn: dsn}
	probe.Close()
	sqlDB := sql.OpenDB(connector)
	t.Cleanup(func() { sqlDB.Close() })
	return sqlDB, connector
}

// burst holds n connections at once, then releases them all, the way n
// concurrent requests each hold one while they run a query.
func burst(t *testing.T, sqlDB *sql.DB, n int) {
	t.Helper()
	var held, release sync.WaitGroup
	held.Add(n)
	release.Add(1)
	var done sync.WaitGroup
	for i := 0; i < n; i++ {
		done.Add(1)
		go func() {
			defer done.Done()
			conn, err := sqlDB.Conn(context.Background())
			held.Done()
			if err != nil {
				t.Error(err)
				return
			}
			release.Wait()
			conn.Close()
		}()
	}
	held.Wait()
	release.Done()
	done.Wait()
}

// serveWhile runs ServeContext against sqlDB with opts and calls during once
// the server has started.
func serveWhile(t *testing.T, sqlDB *sql.DB, during func(), opts ...ServeOption) {
	t.Helper()
	started := make(chan struct{})
	app := tangoAppRegisteringLifecycles(Lifecycle{
		Name:  "signal",
		Start: func(context.Context) error { close(started); return nil },
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- ServeContext(ctx, Config{Addr: "127.0.0.1:0", InstalledApps: []App{app}}, sqlDB, db.SQLite, opts...)
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the server did not start")
	}
	during()
	cancel()
	if err := waitDone(t, done); err != nil {
		t.Fatalf("ServeContext: %v", err)
	}
}

func TestServeContextReusesConnectionsAcrossBursts(t *testing.T) {
	sqlDB, connector := openCounted(t)
	serveWhile(t, sqlDB, func() {
		burst(t, sqlDB, 16)
		burst(t, sqlDB, 16)
	})
	if got := connector.opened.Load(); got != 16 {
		t.Fatalf("connections opened = %d, want 16: the second burst should reuse the first one's connections", got)
	}
}

func TestWithDBPoolZeroValueKeepsTheDatabaseSQLDefaults(t *testing.T) {
	sqlDB, connector := openCounted(t)
	serveWhile(t, sqlDB, func() {
		burst(t, sqlDB, 16)
		burst(t, sqlDB, 16)
	}, WithDBPool(DBPool{}))
	// database/sql keeps 2 idle connections, so the second burst reopens 14.
	if got := connector.opened.Load(); got != 30 {
		t.Fatalf("connections opened = %d, want 30 (16 + 14 reopened)", got)
	}
}

func TestServeContextLeavesAPoolTheAppBoundedAlone(t *testing.T) {
	sqlDB, connector := openCounted(t)
	sqlDB.SetMaxOpenConns(4)
	sqlDB.SetMaxIdleConns(1)
	serveWhile(t, sqlDB, func() {
		burst(t, sqlDB, 4)
		burst(t, sqlDB, 4)
	})
	if got := sqlDB.Stats().MaxOpenConnections; got != 4 {
		t.Fatalf("MaxOpenConnections = %d, want the app's 4", got)
	}
	if got := connector.opened.Load(); got != 7 {
		t.Fatalf("connections opened = %d, want 7 (the app's one idle connection is kept)", got)
	}
}

func TestWithDBPoolSetsTheGivenLimits(t *testing.T) {
	sqlDB, _ := openCounted(t)
	serveWhile(t, sqlDB, func() {
		if got := sqlDB.Stats().MaxOpenConnections; got != 3 {
			t.Errorf("MaxOpenConnections = %d, want 3", got)
		}
	}, WithDBPool(DBPool{MaxOpen: 3, MaxIdle: 3}))
}

func TestWithDBPoolRejectsNegativeValues(t *testing.T) {
	sqlDB, _ := openCounted(t)
	for _, pool := range []DBPool{{MaxOpen: -1}, {MaxIdle: -1}, {MaxLifetime: -time.Second}, {MaxIdleTime: -time.Second}} {
		err := ServeContext(context.Background(), Config{Addr: "127.0.0.1:0"}, sqlDB, db.SQLite, WithDBPool(pool))
		if err == nil {
			t.Errorf("WithDBPool(%+v): expected an error, got nil", pool)
		}
	}
}

func TestWithDBPoolAppliesLifetimeAndIdleTime(t *testing.T) {
	sqlDB, _ := openCounted(t)
	applyDBPool(sqlDB, serveConfig{
		dbPool:    DBPool{MaxLifetime: 30 * time.Millisecond, MaxIdleTime: 30 * time.Millisecond},
		dbPoolSet: true,
	}, db.SQLite)
	if err := sqlDB.Ping(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
		if err := sqlDB.Ping(); err != nil {
			t.Fatal(err)
		}
		stats := sqlDB.Stats()
		if stats.MaxLifetimeClosed > 0 || stats.MaxIdleTimeClosed > 0 {
			return
		}
	}
	t.Fatalf("no connection was closed for age or idleness: %+v", sqlDB.Stats())
}

func TestDefaultPoolLimitsConnectionLifetimeOnlyForMySQL(t *testing.T) {
	tests := []struct {
		dialect db.Dialect
		want    DBPool
	}{
		{db.SQLite, DBPool{MaxIdle: defaultDBMaxIdle}},
		{db.Postgres, DBPool{MaxIdle: defaultDBMaxIdle}},
		// MySQL servers and proxies close idle connections on their own
		// timeout, so a MySQL pool never reuses one for more than 5 minutes.
		{db.MySQL, DBPool{MaxIdle: defaultDBMaxIdle, MaxLifetime: 5 * time.Minute}},
	}
	for _, tt := range tests {
		if got := defaultDBPool(tt.dialect); got != tt.want {
			t.Errorf("defaultDBPool(%v) = %+v, want %+v", tt.dialect, got, tt.want)
		}
	}
}

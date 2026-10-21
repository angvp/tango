package main

import (
	"context"
	"database/sql"
	"runtime/debug"
	"sort"
	"time"

	"github.com/angvp/tango"
	"github.com/angvp/tango/admin"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/migration"

	"api-with-admin/migrations"
)

// started is when this process began, shown on the dashboard's status card.
var started = time.Now()

// dashboardStatus is the admin dashboard's status provider. It is built from
// values main already owns (the database, its dialect and the migration list)
// and from tango.Status, and it hands the dashboard display data only: no
// DSN and no driver error text.
func dashboardStatus(config func() tango.Config, sqlDB *sql.DB, dialect db.Dialect) func(context.Context) (admin.DashboardStatus, error) {
	return func(ctx context.Context) (admin.DashboardStatus, error) {
		project := tango.Status(ctx, config(), sqlDB, dialect, migrations.Migrations)
		status := admin.DashboardStatus{
			Database:      admin.DatabaseUnreachable,
			Dialect:       dialectName(dialect),
			Applied:       project.MigrationsApplied,
			TrackingTable: admin.TrackingTableUnknown,
			Release:       release(),
			Started:       started,
		}
		if !project.DatabaseReachable {
			return status, nil
		}
		status.Database = admin.DatabaseReachable
		pending, tracking, err := pendingMigrations(ctx, sqlDB)
		if err != nil {
			return admin.DashboardStatus{}, err
		}
		status.Pending, status.TrackingTable = pending, tracking
		return status, nil
	}
}

// pendingMigrations names the migrations that have not been applied. A
// database without the tracking table has applied none of them.
func pendingMigrations(ctx context.Context, sqlDB *sql.DB) ([]string, admin.TrackingTableState, error) {
	applied, err := migration.AppliedMigrations(ctx, sqlDB)
	tracking := admin.TrackingTablePresent
	if err != nil {
		if !migration.IsMissingTrackingTable(err) {
			return nil, admin.TrackingTableUnknown, err
		}
		applied, tracking = map[migration.MigrationKey]bool{}, admin.TrackingTableMissing
	}
	var pending []string
	for _, m := range migrations.Migrations {
		if !applied[migration.MigrationKey{App: m.App, Name: m.Name}] {
			pending = append(pending, m.App+"."+m.Name)
		}
	}
	sort.Strings(pending)
	return pending, tracking, nil
}

func dialectName(dialect db.Dialect) string {
	switch dialect {
	case db.SQLite:
		return "SQLite"
	case db.Postgres:
		return "PostgreSQL"
	case db.MySQL:
		return "MySQL"
	}
	return "unknown"
}

// release is the tanGO version this binary was built with.
func release() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, dep := range info.Deps {
			if dep.Path == "github.com/angvp/tango" {
				return dep.Version
			}
		}
	}
	return "development"
}

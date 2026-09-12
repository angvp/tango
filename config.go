package tango

import (
	"context"
	"database/sql"
	"os"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/migration"
)

const defaultAddr = ":8000"

// Config is the minimal application configuration for v0.1.
type Config struct {
	InstalledApps []App
	Addr          string
}

// LoadConfigFromEnv returns a Config populated from environment variables.
func LoadConfigFromEnv() Config {
	config := Config{
		Addr: defaultAddr,
	}

	if addr := os.Getenv("TANGO_ADDR"); addr != "" {
		config.Addr = addr
	}

	return config
}

// BuildRegistry registers Config.InstalledApps into a new Registry.
func BuildRegistry(config Config) (*Registry, error) {
	registry := NewRegistry()

	for _, app := range config.InstalledApps {
		if err := registry.Register(app); err != nil {
			return nil, err
		}
	}

	return registry, nil
}

// Check validates that the configured app registry can boot and compile routes.
func Check(config Config) error {
	registry, err := BuildRegistry(config)
	if err != nil {
		return err
	}

	if err := registry.RunRegistration(); err != nil {
		return err
	}

	_, err = registry.Routes().Handler()
	return err
}

// DumpModels boots config's registry and returns its registered models in
// the dialect-agnostic shape `tango makemigrations` diffs against. This is
// the documented convention behind the "-tango-dump-models" flag: an app's
// main.go handling that flag calls DumpModels, JSON-encodes the result to
// stdout, and exits — mirroring the "-check" flag convention from
// Milestone 6.
func DumpModels(config Config) ([]migration.Model, error) {
	registry, err := BuildRegistry(config)
	if err != nil {
		return nil, err
	}

	if err := registry.RunRegistration(); err != nil {
		return nil, err
	}

	return migration.ModelsFromMeta(registry.Models().All()), nil
}

// ProjectStatus is a snapshot of a project's registration, database, and
// migration state, produced by Status. It is the payload behind the
// documented "-tango-status" flag convention (see Milestone 8.3).
type ProjectStatus struct {
	RegistrationOK    bool   `json:"registrationOk"`
	RegistrationError string `json:"registrationError,omitempty"`
	DatabaseReachable bool   `json:"databaseReachable"`
	DatabaseError     string `json:"databaseError,omitempty"`
	MigrationsTotal   int    `json:"migrationsTotal"`
	MigrationsApplied int    `json:"migrationsApplied"`
	MigrationsPending int    `json:"migrationsPending"`
}

// Status reports config's registration status, sqlDB's reachability, and
// migrations' applied/pending counts, without writing to the database or
// filesystem. This is the documented convention behind the "-tango-status"
// flag: an app's main.go handling that flag calls Status, JSON-encodes the
// result to stdout, and exits — mirroring "-check" and "-tango-dump-models".
func Status(ctx context.Context, config Config, sqlDB *sql.DB, dialect db.Dialect, migrations []migration.Migration) ProjectStatus {
	status := ProjectStatus{MigrationsTotal: len(migrations)}

	if err := Check(config); err != nil {
		status.RegistrationError = err.Error()
	} else {
		status.RegistrationOK = true
	}

	if err := sqlDB.PingContext(ctx); err != nil {
		status.DatabaseError = err.Error()
		return status
	}
	status.DatabaseReachable = true

	if err := migration.EnsureTrackingTable(ctx, sqlDB, dialect); err != nil {
		status.DatabaseReachable = false
		status.DatabaseError = err.Error()
		return status
	}

	applied, err := migration.AppliedMigrations(ctx, sqlDB)
	if err != nil {
		status.DatabaseError = err.Error()
		return status
	}

	for _, m := range migrations {
		if applied[migration.MigrationKey{App: m.App, Name: m.Name}] {
			status.MigrationsApplied++
		}
	}
	status.MigrationsPending = status.MigrationsTotal - status.MigrationsApplied

	return status
}

package tango

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

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

// LoadDBDSNFromEnv returns TANGO_DB_DSN, or an empty string when unset.
func LoadDBDSNFromEnv() string {
	return os.Getenv("TANGO_DB_DSN")
}

// LoadDBDialectFromEnv returns the DB dialect selected by TANGO_DB_DIALECT.
// It defaults to SQLite when unset.
func LoadDBDialectFromEnv() (db.Dialect, error) {
	value := strings.ToLower(strings.TrimSpace(os.Getenv("TANGO_DB_DIALECT")))
	switch value {
	case "", "sqlite":
		return db.SQLite, nil
	case "postgres", "postgresql":
		return db.Postgres, nil
	default:
		return db.SQLite, fmt.Errorf("tango: unsupported TANGO_DB_DIALECT %q", value)
	}
}

// LoadEnvFile loads simple KEY=VALUE lines from path into the process
// environment without overriding variables that are already set.
func LoadEnvFile(path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	for lineNumber, rawLine := range strings.Split(string(content), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return fmt.Errorf("tango: %s:%d: expected KEY=VALUE", path, lineNumber+1)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if len(value) >= 2 {
			if unquoted, err := strconv.Unquote(value); err == nil {
				value = unquoted
			}
		}
		if os.Getenv(key) == "" {
			if err := os.Setenv(key, value); err != nil {
				return err
			}
		}
	}
	return nil
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

// DispatchFlags handles tanGO's app-side CLI flags. It returns handled=true
// when a flag path ran and the caller should exit after checking err.
func DispatchFlags(config Config, sqlDB *sql.DB, dialect db.Dialect, migrations []migration.Migration) (bool, error) {
	flags := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	check := flags.Bool("check", false, "validate app registration and exit")
	dumpModels := flags.Bool("tango-dump-models", false, "print registered models as JSON and exit")
	status := flags.Bool("tango-status", false, "print project status as JSON and exit")
	migrateFlag := flags.Bool("migrate", false, "apply pending migrations and exit")
	down := flags.Bool("down", false, "roll back the last applied migration (with -migrate)")
	if err := flags.Parse(os.Args[1:]); err != nil {
		return true, err
	}

	switch {
	case *dumpModels:
		models, err := DumpModels(config)
		if err != nil {
			return true, err
		}
		return true, json.NewEncoder(os.Stdout).Encode(models)
	case *check:
		if err := Check(config); err != nil {
			return true, fmt.Errorf("check failed: %w", err)
		}
		fmt.Fprintln(os.Stdout, "check passed")
		return true, nil
	case *status:
		result := Status(context.Background(), config, sqlDB, dialect, migrations)
		return true, json.NewEncoder(os.Stdout).Encode(result)
	case *migrateFlag && *down:
		if err := migration.RollbackLast(context.Background(), sqlDB, dialect, migrations); err != nil {
			return true, err
		}
		fmt.Fprintln(os.Stdout, "rolled back last migration")
		return true, nil
	case *migrateFlag:
		if err := migration.ApplyPending(context.Background(), sqlDB, dialect, migrations); err != nil {
			return true, err
		}
		fmt.Fprintln(os.Stdout, "migrations applied")
		return true, nil
	default:
		return false, nil
	}
}

// Serve builds and runs config's registry using sqlDB-backed persistence.
// dialect must match whatever dialect sqlDB was opened with — callers that
// also invoke DispatchFlags should pass the same dialect value to both.
func Serve(config Config, sqlDB *sql.DB, dialect db.Dialect) error {
	registry, err := BuildRegistry(config)
	if err != nil {
		return fmt.Errorf("build registry: %w", err)
	}
	if err := registry.RunRegistration(); err != nil {
		return fmt.Errorf("run registration: %w", err)
	}
	registry.SetStore(db.NewStore(sqlDB, dialect))
	handler, err := registry.Routes().Handler()
	if err != nil {
		return fmt.Errorf("compile routes: %w", err)
	}
	return listenAndServe(config.Addr, handler)
}

var listenAndServe = http.ListenAndServe

// Check validates that the configured app registry can boot, compile routes,
// and pass every AppCheck contributed by installed apps.
func Check(config Config) error {
	registry, err := BuildRegistry(config)
	if err != nil {
		return err
	}

	if err := registry.RunRegistration(); err != nil {
		return err
	}

	if err := registry.Models().ValidateForeignKeys(); err != nil {
		return err
	}

	if _, err := registry.Routes().Handler(); err != nil {
		return err
	}

	return checkAppChecks(registry.Checks())
}

func checkAppChecks(checks []AppCheck) error {
	var failures []string
	for _, check := range checks {
		if check.Err == nil {
			continue
		}
		description := check.Description
		if description == "" {
			description = "unnamed app check"
		}
		failures = append(failures, fmt.Sprintf("%s: %v", description, check.Err))
	}
	if len(failures) == 0 {
		return nil
	}
	return errors.New("tango check failed: " + strings.Join(failures, "; "))
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

	applied, err := migration.AppliedMigrations(ctx, sqlDB)
	if err != nil {
		if migration.IsMissingTrackingTable(err) {
			status.MigrationsPending = status.MigrationsTotal
			return status
		}
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

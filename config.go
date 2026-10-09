package tango

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/migration"
)

const defaultAddr = ":8000"

// Config is the minimal application configuration.
type Config struct {
	InstalledApps []App
	Addr          string
	Middleware    []Middleware
	// MiddlewareScope decides what Middleware wraps: each matched route,
	// or the whole router, Unmatched requests included. The zero value is
	// the framework's default, MiddlewareScopeRoutes.
	MiddlewareScope MiddlewareScope
	// NotFound, if set, answers requests whose path no route matches, in
	// place of the router's plain-text 404. MethodNotAllowed, if set,
	// answers requests whose path a route matches but not their method, in
	// place of the router's empty 405; tanGO sets the Allow header before
	// it runs. Both run like any View (an error gets the generic 500).
	// Under MiddlewareScopeAll they run inside Middleware and are observed
	// as route "(unmatched)"; under MiddlewareScopeRoutes they run outside
	// it and are not observed.
	NotFound         View
	MethodNotAllowed View
}

// MiddlewareScope is what Config.Middleware wraps.
type MiddlewareScope int

const (
	// MiddlewareScopeDefault is the framework's default scope, currently
	// MiddlewareScopeRoutes. A later release may change the default, with a
	// minor release's notice in the changelog; set a scope explicitly to
	// keep it.
	MiddlewareScopeDefault MiddlewareScope = iota
	// MiddlewareScopeRoutes wraps each matched route. Unmatched requests,
	// which the router answers itself with 404 or 405, pass no global
	// middleware and are neither logged nor counted.
	MiddlewareScopeRoutes
	// MiddlewareScopeAll wraps the whole router, so Unmatched requests pass
	// global middleware too. A request's route is resolved before global
	// middleware runs, from the request as it arrived: an Unmatched request
	// is reported as route "(unmatched)". Middleware that rewrites the method
	// or path so the router dispatches elsewhere is unsupported, and global
	// middleware sees no route parameters: the router hasn't dispatched yet.
	MiddlewareScopeAll
)

// resolve returns the scope s stands for: MiddlewareScopeDefault becomes
// the framework's default.
func (s MiddlewareScope) resolve() (MiddlewareScope, error) {
	switch s {
	case MiddlewareScopeDefault:
		return MiddlewareScopeRoutes, nil
	case MiddlewareScopeRoutes, MiddlewareScopeAll:
		return s, nil
	}
	return 0, fmt.Errorf("tango: unknown MiddlewareScope %d", int(s))
}

// ConfigOption changes how LoadConfigFromEnv reads the environment.
type ConfigOption func(*envConfig)

type envConfig struct {
	portFromEnv bool
}

// WithPortFromEnv makes LoadConfigFromEnv fall back to the PORT variable
// that hosting platforms (Railway, Heroku, Cloud Run, ...) set: the
// address is TANGO_ADDR if set, else ":"+PORT if PORT is set, else :8000.
func WithPortFromEnv() ConfigOption {
	return func(c *envConfig) { c.portFromEnv = true }
}

// LoadConfigFromEnv returns a Config populated from environment variables:
// Addr is TANGO_ADDR, defaulting to :8000. WithPortFromEnv adds PORT as a
// fallback before the default.
func LoadConfigFromEnv(opts ...ConfigOption) Config {
	var env envConfig
	for _, opt := range opts {
		opt(&env)
	}
	config := Config{
		Addr: defaultAddr,
	}

	if addr := os.Getenv("TANGO_ADDR"); addr != "" {
		config.Addr = addr
	} else if port := os.Getenv("PORT"); env.portFromEnv && port != "" {
		config.Addr = ":" + port
	}

	return config
}

// defaultDBDSN is the database an app opens when TANGO_DB_DSN is unset.
const defaultDBDSN = "sqlite://app.db"

// LoadDBConfigFromEnv parses TANGO_DB_DSN with db.ParseDSN and returns the
// dialect, database/sql driver name, and driver data source name to open
// the app's database with. It defaults to sqlite://app.db when TANGO_DB_DSN is unset.
// The app still imports and registers the driver itself.
//
// It fails when the retired TANGO_DB_DIALECT is set, so a deployment that
// still relies on it stops at startup instead of opening the wrong database.
func LoadDBConfigFromEnv() (db.DSN, error) {
	if os.Getenv("TANGO_DB_DIALECT") != "" {
		return db.DSN{}, errors.New("tango: TANGO_DB_DIALECT was removed; put the scheme in TANGO_DB_DSN (e.g. postgres://…)")
	}
	dsn := os.Getenv("TANGO_DB_DSN")
	if dsn == "" {
		dsn = defaultDBDSN
	}
	parsed, err := db.ParseDSN(dsn)
	if err != nil {
		return db.DSN{}, fmt.Errorf("tango: TANGO_DB_DSN: %w", err)
	}
	return parsed, nil
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
	scope, err := config.MiddlewareScope.resolve()
	if err != nil {
		return nil, err
	}
	registry := NewRegistry()
	registry.Routes().setRouterConfig(routerConfig{
		middleware:       config.Middleware,
		scope:            scope,
		notFound:         config.NotFound,
		methodNotAllowed: config.MethodNotAllowed,
	})

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
		applied, err := applyPendingReporting(context.Background(), sqlDB, dialect, migrations)
		if err != nil {
			return true, err
		}
		fmt.Fprintln(os.Stdout, describeApplied(applied))
		return true, nil
	default:
		return false, nil
	}
}

// Serve builds and runs config's registry using sqlDB-backed persistence.
// dialect must match whatever dialect sqlDB was opened with — callers that
// also invoke DispatchFlags should pass the same dialect value to both.
//
// Serve blocks forever: it delegates to ServeContext with a background
// context, which never cancels, so there is no caller-triggered shutdown
// path. Callers that want graceful shutdown call ServeContext directly with
// a cancelable context (e.g. one built from signal.NotifyContext).
func Serve(config Config, sqlDB *sql.DB, dialect db.Dialect) error {
	return ServeContext(context.Background(), config, sqlDB, dialect)
}

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
// stdout, and exits — mirroring the "-check" flag convention.
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
// documented "-tango-status" flag convention, which `tango tui` reads.
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

// applyPendingReporting applies pending migrations and returns the "app/name"
// of each one this call applied, in the order migrations lists them.
func applyPendingReporting(ctx context.Context, sqlDB *sql.DB, dialect db.Dialect, migrations []migration.Migration) ([]string, error) {
	before, err := migration.AppliedMigrations(ctx, sqlDB)
	if err != nil && !migration.IsMissingTrackingTable(err) {
		return nil, err
	}
	if err := migration.ApplyPending(ctx, sqlDB, dialect, migrations); err != nil {
		return nil, err
	}
	after, err := migration.AppliedMigrations(ctx, sqlDB)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, m := range migrations {
		key := migration.MigrationKey{App: m.App, Name: m.Name}
		if after[key] && !before[key] {
			names = append(names, m.App+"/"+m.Name)
		}
	}
	return names, nil
}

func describeApplied(names []string) string {
	switch len(names) {
	case 0:
		return "no pending migrations"
	case 1:
		return "applied 1 migration: " + names[0]
	default:
		return fmt.Sprintf("applied %d migrations: %s", len(names), strings.Join(names, ", "))
	}
}

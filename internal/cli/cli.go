package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/format"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/angvp/tango/migration"
)

// Runner executes an external command.
type Runner interface {
	Run(ctx context.Context, dir string, name string, args []string, stdout io.Writer, stderr io.Writer) error
}

// ExecRunner runs commands with os/exec.
type ExecRunner struct{}

// Run executes name with args in dir, wiring stdout and stderr through.
func (ExecRunner) Run(ctx context.Context, dir string, name string, args []string, stdout io.Writer, stderr io.Writer) error {
	command := exec.CommandContext(ctx, name, args...)
	command.Dir = dir
	command.Stdout = stdout
	command.Stderr = stderr
	command.Stdin = os.Stdin
	return command.Run()
}

// Run executes the tango CLI and returns a process-style exit code.
func Run(ctx context.Context, args []string, dir string, stdout io.Writer, stderr io.Writer, runner Runner) int {
	if runner == nil {
		runner = ExecRunner{}
	}

	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		printUsage(stdout)
		return 0
	}

	switch args[0] {
	case "run":
		return runGo(ctx, runner, dir, stdout, stderr, append([]string{"run", "."}, args[1:]...))
	case "check":
		return runGo(ctx, runner, dir, stdout, stderr, append([]string{"run", ".", "-check"}, args[1:]...))
	case "makemigrations":
		return makeMigrations(ctx, runner, dir, args[1:], stdout, stderr)
	case "newproject":
		return newProject(ctx, runner, dir, args[1:], stdout, stderr)
	case "newapp":
		if len(args) < 2 {
			fmt.Fprintln(stderr, "tango newapp: an app name is required")
			return 2
		}
		return newApp(dir, args[1], stdout, stderr)
	case "tui":
		return tui(ctx, runner, dir, stdout, stderr, isInteractiveTerminal)
	case "shell":
		fmt.Fprintln(stderr, "tango shell is not implemented yet; Milestone 6 records Yaegi as the intended direction.")
		return 2
	case "migrate":
		if len(args) > 1 && args[1] == "down" {
			return runGo(ctx, runner, dir, stdout, stderr, append([]string{"run", ".", "-migrate", "-down"}, args[2:]...))
		}
		return runGo(ctx, runner, dir, stdout, stderr, append([]string{"run", ".", "-migrate"}, args[1:]...))
	case "admin":
		return adminCommand(ctx, runner, dir, args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n", args[0])
		printUsage(stderr)
		return 2
	}
}

func runGo(ctx context.Context, runner Runner, dir string, stdout io.Writer, stderr io.Writer, args []string) int {
	if err := runner.Run(ctx, dir, "go", args, stdout, stderr); err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			return exitError.ExitCode()
		}
		fmt.Fprintf(stderr, "tango: %v\n", err)
		return 1
	}

	return 0
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `Usage:
  tango run [args...]      Run the current Go app with go run .
  tango check [args...]    Run the current Go app with -check
  tango makemigrations [--name <name>]
                           Generate a migration from current model metadata
  tango migrate            Apply pending migrations (go run . -migrate)
  tango migrate down       Roll back the last applied migration
  tango newproject [--dialect=sqlite|postgres] [--no-admin] <name>
                           Scaffold a new runnable project
  tango newapp <name>      Scaffold a new app stub in the current project
  tango tui                Open a status dashboard (falls back to plain text)
  tango shell              Not implemented; Yaegi is the intended direction
  tango admin create <username> [--no-staff] [--no-superuser]
                                          Create an admin account (password via stdin);
                                          defaults to staff+superuser access
  tango admin resetpassword <username>   Reset an admin account's password (via stdin)
  tango admin deactivate <username>      Deactivate an admin account
  tango admin grant-staff <username>     Grant staff access (can access the admin panel)
  tango admin revoke-staff <username>    Revoke staff access
  tango admin grant-superuser <username> Grant superuser access (reserved; no effect yet)
  tango admin revoke-superuser <username> Revoke superuser access
`)
}

func adminCommand(ctx context.Context, runner Runner, dir string, args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) < 2 {
		fmt.Fprintln(stderr, "tango admin: usage: tango admin <verb> <username>")
		return 2
	}

	verb, username := args[0], args[1]
	if verb == "create" {
		flags := []string{"run", ".", "-tango-admin-create=" + username}
		for _, extra := range args[2:] {
			switch extra {
			case "--no-staff":
				flags = append(flags, "-tango-admin-no-staff")
			case "--no-superuser":
				flags = append(flags, "-tango-admin-no-superuser")
			default:
				fmt.Fprintf(stderr, "tango admin create: unknown flag %q (want --no-staff or --no-superuser)\n", extra)
				return 2
			}
		}
		return runGo(ctx, runner, dir, stdout, stderr, flags)
	}

	var flagName string
	switch verb {
	case "resetpassword":
		flagName = "-tango-admin-resetpassword"
	case "deactivate":
		flagName = "-tango-admin-deactivate"
	case "grant-staff":
		flagName = "-tango-admin-grant-staff"
	case "revoke-staff":
		flagName = "-tango-admin-revoke-staff"
	case "grant-superuser":
		flagName = "-tango-admin-grant-superuser"
	case "revoke-superuser":
		flagName = "-tango-admin-revoke-superuser"
	default:
		fmt.Fprintf(stderr, "tango admin: unknown subcommand %q (want create, resetpassword, deactivate, grant-staff, revoke-staff, grant-superuser, or revoke-superuser)\n", verb)
		return 2
	}

	if len(args) > 2 {
		fmt.Fprintf(stderr, "tango admin %s: unexpected arguments: %s (only create accepts flags)\n", verb, strings.Join(args[2:], " "))
		return 2
	}

	return runGo(ctx, runner, dir, stdout, stderr, []string{"run", ".", flagName + "=" + username})
}

func makeMigrations(ctx context.Context, runner Runner, dir string, args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("makemigrations", flag.ContinueOnError)
	flags.SetOutput(stderr)
	explicitName := flags.String("name", "", "use a descriptive migration name")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "tango makemigrations: unexpected arguments: %s\n", strings.Join(flags.Args(), " "))
		return 2
	}

	nameProvided := false
	flags.Visit(func(current *flag.Flag) {
		if current.Name == "name" {
			nameProvided = true
		}
	})
	var sanitizedName string
	if nameProvided {
		var err error
		sanitizedName, err = sanitizeMigrationName(*explicitName)
		if err != nil {
			fmt.Fprintf(stderr, "tango makemigrations: %v\n", err)
			return 2
		}
	}

	models, err := dumpModels(ctx, runner, dir, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "tango makemigrations: %v\n", err)
		return 1
	}

	existing, err := loadMigrations(dir)
	if err != nil {
		fmt.Fprintf(stderr, "tango makemigrations: %v\n", err)
		return 1
	}

	state, err := migration.Replay(existing)
	if err != nil {
		fmt.Fprintf(stderr, "tango makemigrations: %v\n", err)
		return 1
	}

	changes := migration.DiffModels(models, state)
	if len(changes) == 0 {
		fmt.Fprintln(stdout, "no changes detected")
		return 0
	}

	// One file per app: each element of changes is already one app's
	// Migration (Diff groups by app), so each gets its own sequential
	// name and its own file rather than sharing one name/file across
	// apps (see Milestone 8.1).
	for _, change := range changes {
		sequence, err := nextMigrationSequence(dir)
		if err != nil {
			fmt.Fprintf(stderr, "tango makemigrations: %v\n", err)
			return 1
		}

		var name, filename string
		if nameProvided {
			name, filename, err = explicitMigrationTarget(dir, sequence, sanitizedName)
		} else {
			name, filename, err = autoMigrationTarget(dir, sequence, time.Now().UTC())
		}
		if err != nil {
			fmt.Fprintf(stderr, "tango makemigrations: %v\n", err)
			return 1
		}
		change.Name = name

		if err := writeMigrationFile(filename, migrationVarName(name), []migration.Migration{change}); err != nil {
			fmt.Fprintf(stderr, "tango makemigrations: %v\n", err)
			return 1
		}

		rel, err := filepath.Rel(dir, filename)
		if err != nil {
			rel = filename
		}
		fmt.Fprintf(stdout, "created %s\n", filepath.ToSlash(rel))
	}

	if err := regenerateMigrationsAggregate(dir); err != nil {
		fmt.Fprintf(stderr, "tango makemigrations: %v\n", err)
		return 1
	}
	return 0
}

func dumpModels(ctx context.Context, runner Runner, dir string, stderr io.Writer) ([]migration.Model, error) {
	var stdout bytes.Buffer
	if err := runner.Run(ctx, dir, "go", []string{"run", ".", "-tango-dump-models"}, &stdout, stderr); err != nil {
		return nil, err
	}

	var models []migration.Model
	if err := json.Unmarshal(stdout.Bytes(), &models); err != nil {
		return nil, fmt.Errorf("decode model manifest: %w", err)
	}
	return models, nil
}

var (
	migrationPrefixPattern      = regexp.MustCompile(`^(\d{4})_.*\.go$`)
	invalidMigrationNamePattern = regexp.MustCompile(`[^a-z0-9_]`)
	migrationUnderscorePattern  = regexp.MustCompile(`_+`)
)

func nextMigrationSequence(dir string) (int, error) {
	migrationsDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(migrationsDir, 0o755); err != nil {
		return 0, err
	}

	files, err := filepath.Glob(filepath.Join(migrationsDir, "*.go"))
	if err != nil {
		return 0, err
	}

	maxPrefix := 0
	for _, file := range files {
		match := migrationPrefixPattern.FindStringSubmatch(filepath.Base(file))
		if len(match) != 2 {
			continue
		}
		var prefix int
		if _, err := fmt.Sscanf(match[1], "%d", &prefix); err != nil {
			return 0, err
		}
		if prefix > maxPrefix {
			maxPrefix = prefix
		}
	}

	return maxPrefix + 1, nil
}

func autoMigrationTarget(dir string, sequence int, now time.Time) (string, string, error) {
	for attempt := 0; attempt < 5; attempt++ {
		timestamp := now.Add(time.Duration(attempt) * time.Second).UTC().Format("20060102150405")
		name := fmt.Sprintf("%04d_auto_%s", sequence, timestamp)
		filename := filepath.Join(dir, "migrations", name+".go")
		if _, err := os.Stat(filename); errors.Is(err, os.ErrNotExist) {
			return name, filename, nil
		} else if err != nil {
			return "", "", fmt.Errorf("check migration file %q: %w", filename, err)
		}
	}

	return "", "", fmt.Errorf("could not create an automatic migration for sequence %04d: all 5 timestamp candidates already exist", sequence)
}

func explicitMigrationTarget(dir string, sequence int, suffix string) (string, string, error) {
	name := fmt.Sprintf("%04d_%s", sequence, suffix)
	filename := filepath.Join(dir, "migrations", name+".go")
	if _, err := os.Stat(filename); err == nil {
		return "", "", fmt.Errorf("migration file %q already exists; choose a different --name", filename)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", "", fmt.Errorf("check migration file %q: %w", filename, err)
	}
	return name, filename, nil
}

func sanitizeMigrationName(input string) (string, error) {
	normalized := strings.ToLower(input)
	normalized = strings.NewReplacer(" ", "_", "-", "_").Replace(normalized)
	if invalid := invalidMigrationNamePattern.FindString(normalized); invalid != "" {
		return "", fmt.Errorf("invalid migration name %q: character %q is not allowed; use letters, numbers, spaces, hyphens, or underscores", input, invalid)
	}
	normalized = migrationUnderscorePattern.ReplaceAllString(normalized, "_")
	normalized = strings.Trim(normalized, "_")
	if normalized == "" {
		return "", fmt.Errorf("invalid migration name %q: name is empty after sanitization", input)
	}
	return normalized, nil
}

const migrationJSONPrefix = "// tango:migration-json "

func loadMigrations(dir string) ([]migration.Migration, error) {
	files, err := filepath.Glob(filepath.Join(dir, "migrations", "*.go"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)

	var migrations []migration.Migration
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		for _, line := range strings.Split(string(content), "\n") {
			if !strings.HasPrefix(line, migrationJSONPrefix) {
				continue
			}
			decoded, err := decodeMigrations([]byte(strings.TrimPrefix(line, migrationJSONPrefix)))
			if err != nil {
				return nil, fmt.Errorf("%s: %w", file, err)
			}
			migrations = append(migrations, decoded...)
			break
		}
	}
	return migrations, nil
}

// writeMigrationFile writes one generated migration file. Its Go variable is
// named after varName (unique per file, e.g. "M0001Auto"), never the shared
// "Migrations" name — every migration file lives in the same "migrations"
// package, so a shared name would redeclare across files as soon as a
// project has more than one migration. The single source of truth an app's
// main.go imports is migrations.go's "Migrations" slice, rebuilt by
// regenerateMigrationsAggregate after every write.
func writeMigrationFile(filename string, varName string, migrations []migration.Migration) error {
	encoded, err := encodeMigrations(migrations)
	if err != nil {
		return err
	}
	metadata, err := json.Marshal(encoded)
	if err != nil {
		return err
	}

	var builder strings.Builder
	builder.WriteString("package migrations\n\n")
	builder.WriteString("import \"github.com/angvp/tango/migration\"\n\n")
	builder.WriteString(migrationJSONPrefix)
	builder.Write(metadata)
	builder.WriteString("\n")
	fmt.Fprintf(&builder, "var %s = []migration.Migration{\n", varName)
	for _, m := range migrations {
		writeMigrationLiteral(&builder, m)
	}
	builder.WriteString("}\n")

	formatted, err := format.Source([]byte(builder.String()))
	if err != nil {
		return err
	}
	file, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("migration file %q already exists; refusing to overwrite it", filename)
		}
		return err
	}
	if _, err := file.Write(formatted); err != nil {
		_ = file.Close()
		_ = os.Remove(filename)
		return err
	}
	return file.Close()
}

// migrationVarName derives a unique, exported Go identifier for name (a
// migration file stem like "0001_auto"), in pure UpperCamelCase — never
// mixed with snake_case — so it reads as an ordinary Go export.
func migrationVarName(name string) string {
	var builder strings.Builder
	builder.WriteByte('M')
	capitalizeNext := true
	for _, r := range name {
		if r == '_' {
			capitalizeNext = true
			continue
		}
		if capitalizeNext {
			builder.WriteRune(unicode.ToUpper(r))
			capitalizeNext = false
			continue
		}
		builder.WriteRune(r)
	}
	return builder.String()
}

// regenerateMigrationsAggregate rewrites migrations/migrations.go's
// "Migrations" slice from every migration file currently on disk (via
// loadMigrations, which reads each file's JSON metadata comment). This is
// the one file an app's main.go actually imports; per-file variables exist
// only for human readability of the generated diff.
func regenerateMigrationsAggregate(dir string) error {
	all, err := loadMigrations(dir)
	if err != nil {
		return err
	}

	var builder strings.Builder
	builder.WriteString("package migrations\n\n")
	builder.WriteString("import \"github.com/angvp/tango/migration\"\n\n")
	builder.WriteString("// Migrations is every migration in this project, in application order.\n")
	builder.WriteString("// It is regenerated by `tango makemigrations` — do not hand-edit it.\n")
	builder.WriteString("var Migrations = []migration.Migration{\n")
	for _, m := range all {
		writeMigrationLiteral(&builder, m)
	}
	builder.WriteString("}\n")

	formatted, err := format.Source([]byte(builder.String()))
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "migrations", "migrations.go"), formatted, 0o644)
}

func writeMigrationLiteral(builder *strings.Builder, m migration.Migration) {
	fmt.Fprintf(builder, "{App: %q, Name: %q, Reversible: %t,\n", m.App, m.Name, m.Reversible)
	builder.WriteString("Up: []migration.Step{\n")
	for _, step := range m.Up {
		writeStepLiteral(builder, step)
	}
	builder.WriteString("},\nDown: []migration.Step{\n")
	for _, step := range m.Down {
		writeStepLiteral(builder, step)
	}
	builder.WriteString("},\n},\n")
}

func writeStepLiteral(builder *strings.Builder, step migration.Step) {
	switch s := step.(type) {
	case migration.CreateTable:
		fmt.Fprintf(builder, "migration.CreateTable{Table: %q, Columns: []migration.Column{", s.Table)
		for _, c := range s.Columns {
			// Elements of an already-typed []migration.Column slice omit the
			// redundant migration.Column prefix (gofmt/gopls's
			// simplifycompositelit convention).
			writeColumnFields(builder, c)
		}
		builder.WriteString("}},\n")
	case migration.DropTable:
		fmt.Fprintf(builder, "migration.DropTable{Table: %q},\n", s.Table)
	case migration.AddColumn:
		// Column is a single struct-typed field here, not a slice element,
		// so its type is not redundant and must stay.
		fmt.Fprintf(builder, "migration.AddColumn{Table: %q, Column: migration.Column", s.Table)
		writeColumnFields(builder, s.Column)
		builder.WriteString("},\n")
	case migration.DropColumn:
		fmt.Fprintf(builder, "migration.DropColumn{Table: %q, Column: %q},\n", s.Table, s.Column)
	case migration.AlterColumnUnique:
		fmt.Fprintf(builder, "migration.AlterColumnUnique{Table: %q, Column: %q, Unique: %t},\n", s.Table, s.Column, s.Unique)
	case migration.CreateIndex:
		fmt.Fprintf(builder, "migration.CreateIndex{Table: %q, Column: %q},\n", s.Table, s.Column)
	case migration.DropIndex:
		fmt.Fprintf(builder, "migration.DropIndex{Table: %q, Column: %q},\n", s.Table, s.Column)
	}
}

// writeColumnFields writes a migration.Column literal's field list,
// including the surrounding braces, but never the "migration.Column" type
// prefix — callers add that prefix themselves when it isn't already
// supplied by an enclosing []migration.Column slice's element type.
func writeColumnFields(builder *strings.Builder, c migration.Column) {
	fmt.Fprintf(builder, "{Name: %q, Type: %q, PrimaryKey: %t, Unique: %t, Indexed: %t", c.Name, c.Type, c.PrimaryKey, c.Unique, c.Indexed)
	if c.References != "" {
		fmt.Fprintf(builder, ", References: %q", c.References)
	}
	if c.Default != "" {
		fmt.Fprintf(builder, ", Default: %q", c.Default)
	}
	builder.WriteString("},")
}

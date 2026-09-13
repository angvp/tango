package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/format"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
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
		return makeMigrations(ctx, runner, dir, stdout, stderr)
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
  tango makemigrations     Generate a migration from current model metadata
  tango migrate            Apply pending migrations (go run . -migrate)
  tango migrate down       Roll back the last applied migration
  tango newproject [--dialect=sqlite|postgres] [--no-admin] <name>
                           Scaffold a new runnable project
  tango newapp <name>      Scaffold a new app stub in the current project
  tango tui                Open a status dashboard (falls back to plain text)
  tango shell              Not implemented; Yaegi is the intended direction
  tango admin create <username>          Create an admin account (password via stdin)
  tango admin resetpassword <username>   Reset an admin account's password (via stdin)
  tango admin deactivate <username>      Deactivate an admin account
`)
}

func adminCommand(ctx context.Context, runner Runner, dir string, args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) < 2 {
		fmt.Fprintln(stderr, "tango admin: usage: tango admin <create|resetpassword|deactivate> <username>")
		return 2
	}

	verb, username := args[0], args[1]
	var flagName string
	switch verb {
	case "create":
		flagName = "-tango-admin-create"
	case "resetpassword":
		flagName = "-tango-admin-resetpassword"
	case "deactivate":
		flagName = "-tango-admin-deactivate"
	default:
		fmt.Fprintf(stderr, "tango admin: unknown subcommand %q (want create, resetpassword, or deactivate)\n", verb)
		return 2
	}

	return runGo(ctx, runner, dir, stdout, stderr, []string{"run", ".", flagName + "=" + username})
}

func makeMigrations(ctx context.Context, runner Runner, dir string, stdout io.Writer, stderr io.Writer) int {
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
		name, filename, err := nextMigrationName(dir)
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

func nextMigrationName(dir string) (string, string, error) {
	migrationsDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(migrationsDir, 0o755); err != nil {
		return "", "", err
	}

	files, err := filepath.Glob(filepath.Join(migrationsDir, "*.go"))
	if err != nil {
		return "", "", err
	}

	maxPrefix := 0
	prefixPattern := regexp.MustCompile(`^(\d{4})_.*\.go$`)
	for _, file := range files {
		match := prefixPattern.FindStringSubmatch(filepath.Base(file))
		if len(match) != 2 {
			continue
		}
		var prefix int
		if _, err := fmt.Sscanf(match[1], "%d", &prefix); err != nil {
			return "", "", err
		}
		if prefix > maxPrefix {
			maxPrefix = prefix
		}
	}

	name := fmt.Sprintf("%04d_auto", maxPrefix+1)
	return name, filepath.Join(migrationsDir, name+".go"), nil
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
	return os.WriteFile(filename, formatted, 0o644)
}

// migrationVarName derives a unique, exported Go identifier for name (a
// migration file stem like "0001_auto"), in pure UpperCamelCase per
// AI_COLLABORATION.md.
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
			writeColumnLiteral(builder, c)
		}
		builder.WriteString("}},\n")
	case migration.DropTable:
		fmt.Fprintf(builder, "migration.DropTable{Table: %q},\n", s.Table)
	case migration.AddColumn:
		fmt.Fprintf(builder, "migration.AddColumn{Table: %q, Column: ", s.Table)
		writeColumnLiteral(builder, s.Column)
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

func writeColumnLiteral(builder *strings.Builder, c migration.Column) {
	fmt.Fprintf(builder, "migration.Column{Name: %q, Type: %q, PrimaryKey: %t, Unique: %t, Indexed: %t", c.Name, c.Type, c.PrimaryKey, c.Unique, c.Indexed)
	if c.References != "" {
		fmt.Fprintf(builder, ", References: %q", c.References)
	}
	builder.WriteString("},")
}

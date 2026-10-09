package shell

import (
	"bytes"
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/internal/shellcore"
	"github.com/angvp/tango/testdb"
)

func runShell(t *testing.T, config tango.Config, stdin string, opts Options, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = run(context.Background(), config, testdb.Store(t), args, opts,
		shellcore.IO{In: strings.NewReader(stdin), Out: &out, Err: &errOut})
	return code, out.String(), errOut.String()
}

func TestRunBootsTheProjectAndEvaluates(t *testing.T) {
	registered := false
	config := tango.Config{InstalledApps: []tango.App{tango.NewApp("blog", func(*tango.Registry) error {
		registered = true
		return nil
	})}}
	code, out, errOut := runShell(t, config, "", Options{}, "-c", "6 * 7")
	if code != 0 || out != "42\n" || errOut != "" {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
	if !registered {
		t.Fatal("the shell did not run the apps' registration")
	}
}

func TestRunDoesNotServeAnything(t *testing.T) {
	// A Lifecycle or Job started by serving would show up as a Start call.
	started := false
	app := tango.NewApp("worker", func(r *tango.Registry) error {
		return r.RegisterLifecycle(tango.Lifecycle{Name: "w", Start: func(context.Context) error { started = true; return nil }})
	})
	code, _, errOut := runShell(t, tango.Config{InstalledApps: []tango.App{app}, Addr: "127.0.0.1:0"}, "", Options{}, "-c", "1")
	if code != 0 {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	if started {
		t.Fatal("the shell started an application lifecycle; it must only register")
	}
}

func TestRunReportsAProjectThatFailsToBoot(t *testing.T) {
	config := tango.Config{InstalledApps: []tango.App{tango.NewApp("broken", func(*tango.Registry) error {
		return errors.New("cannot register")
	})}}
	code, out, errOut := runShell(t, config, "", Options{}, "-c", "1")
	if code != 1 || out != "" || !strings.Contains(errOut, "tango shell: run registration: cannot register") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
}

func TestRunRefusesAMissingStore(t *testing.T) {
	var errOut bytes.Buffer
	code := run(context.Background(), tango.Config{}, nil, []string{"-c", "1"}, Options{},
		shellcore.IO{In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &errOut})
	if code != 1 || !strings.Contains(errOut.String(), "no database store") {
		t.Fatalf("code=%d err=%q", code, errOut.String())
	}
}

func TestRunPassesTheCommandLineThrough(t *testing.T) {
	code, out, _ := runShell(t, tango.Config{}, "", Options{}, "--help")
	if code != 0 || !strings.Contains(out, "usage: tango shell") {
		t.Fatalf("code=%d out=%q", code, out)
	}
	code, _, errOut := runShell(t, tango.Config{}, "", Options{}, "--nope")
	if code != 2 || !strings.Contains(errOut, "usage: tango shell") {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
}

// The package's whole public surface is Run and Options with its fields:
// they are Covered API, and anything else here belongs in internal/shellcore.
func TestExportedSurfaceIsRunAndOptionsOnly(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool { return !strings.HasSuffix(fi.Name(), "_test.go") }, 0)
	if err != nil {
		t.Fatal(err)
	}
	var exported, optionFields []string
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				switch d := decl.(type) {
				case *ast.FuncDecl:
					if d.Recv == nil && d.Name.IsExported() {
						exported = append(exported, d.Name.Name)
					}
					if d.Recv != nil && d.Name.IsExported() {
						exported = append(exported, "method "+d.Name.Name)
					}
				case *ast.GenDecl:
					for _, spec := range d.Specs {
						switch sp := spec.(type) {
						case *ast.TypeSpec:
							if !sp.Name.IsExported() {
								continue
							}
							exported = append(exported, sp.Name.Name)
							if st, ok := sp.Type.(*ast.StructType); ok && sp.Name.Name == "Options" {
								for _, f := range st.Fields.List {
									for _, n := range f.Names {
										optionFields = append(optionFields, n.Name)
									}
								}
							}
						case *ast.ValueSpec:
							for _, n := range sp.Names {
								if n.IsExported() {
									exported = append(exported, n.Name)
								}
							}
						}
					}
				}
			}
		}
	}
	slices.Sort(exported)
	slices.Sort(optionFields)
	if want := []string{"Options", "Run"}; !slices.Equal(exported, want) {
		t.Errorf("exported identifiers = %v, want %v", exported, want)
	}
	if want := []string{"DatabaseLabel", "Helpers", "ReadOnly"}; !slices.Equal(optionFields, want) {
		t.Errorf("Options fields = %v, want %v", optionFields, want)
	}
}

func TestServersDoNotLinkTheInterpreter(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go list")
	}
	for _, pkg := range []string{"github.com/angvp/tango", "github.com/angvp/tango/admin", "github.com/angvp/tango/accounts", "github.com/angvp/tango/internal/cli", "github.com/angvp/tango/cmd/tango"} {
		out, err := exec.Command("go", "list", "-deps", pkg).CombinedOutput()
		if err != nil {
			t.Fatalf("go list -deps %s: %v\n%s", pkg, err, out)
		}
		deps := strings.Fields(string(out))
		for _, dep := range deps {
			if strings.HasPrefix(dep, "github.com/traefik/yaegi") || dep == "github.com/angvp/tango/shell" || dep == "github.com/angvp/tango/internal/shellcore" {
				t.Errorf("%s depends on %s: servers and the tango command must not link the interpreter", pkg, dep)
			}
		}
		if !slices.Contains(deps, pkg) {
			t.Errorf("go list -deps %s did not list the package itself", pkg)
		}
	}
}

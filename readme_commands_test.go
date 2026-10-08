package tango_test

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// commandWorld is what README commands are checked against: which
// directories exist, and for a directory, whether it's a main package and
// which flags its binary accepts. Paths are relative to the repository
// root.
type commandWorld struct {
	isDir     func(dir string) bool
	mainFlags func(dir string) (flags []string, isMain bool)
}

var shellFence = regexp.MustCompile("^```(sh|bash|shell|console)\\s*$")

// readmeCommandProblems checks the shell blocks of a README in dir: every
// cd leads to a directory, every `go run .` runs a main package, and every
// flag passed to it is one that binary declares. A `git clone` of this
// repository makes `cd tango/…` relative to the repository root, and a
// directory `tango newproject` or `mkdir` creates counts as existing, its
// main package trusted to the scaffolder. Commands aren't run.
func readmeCommandProblems(dir, text string, world commandWorld) []string {
	var problems []string
	inBlock, cwd, cloned := false, dir, false
	created := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case !inBlock && shellFence.MatchString(trimmed):
			inBlock, cwd, cloned = true, dir, false
			created = map[string]bool{}
			continue
		case inBlock && trimmed == "```":
			inBlock = false
			continue
		case !inBlock:
			continue
		}
		for _, command := range strings.Split(strings.TrimPrefix(trimmed, "$ "), "&&") {
			fields := strings.Fields(command)
			switch {
			case len(fields) == 0 || strings.HasPrefix(fields[0], "#"):
			case len(fields) >= 3 && fields[0] == "git" && fields[1] == "clone" && strings.TrimSuffix(fields[2], ".git") == "https://github.com/angvp/tango":
				cloned = true
			case len(fields) >= 3 && fields[0] == "tango" && fields[1] == "newproject":
				created[path.Join(cwd, fields[len(fields)-1])] = true
			case len(fields) == 2 && fields[0] == "mkdir":
				created[path.Join(cwd, fields[1])] = true
			case fields[0] == "cd" && len(fields) == 2:
				target := path.Join(cwd, fields[1])
				if cloned && (fields[1] == "tango" || strings.HasPrefix(fields[1], "tango/")) {
					target = path.Clean(strings.TrimPrefix(strings.TrimPrefix(fields[1], "tango"), "/"))
				}
				if !world.isDir(target) && !created[target] {
					problems = append(problems, "cd "+fields[1]+": no such directory")
					continue
				}
				cwd = target
			case len(fields) >= 3 && fields[0] == "go" && fields[1] == "run" && fields[2] == "." && created[cwd]:
			case len(fields) >= 3 && fields[0] == "go" && fields[1] == "run" && fields[2] == ".":
				flags, isMain := world.mainFlags(cwd)
				if !isMain {
					problems = append(problems, "go run . in "+cwd+": not a main package")
					continue
				}
				for _, arg := range fields[3:] {
					if !strings.HasPrefix(arg, "-") {
						continue
					}
					name, _, _ := strings.Cut(strings.TrimLeft(arg, "-"), "=")
					if !slices.Contains(flags, name) {
						problems = append(problems, "go run . "+arg+" in "+cwd+": "+cwd+" declares no such flag")
					}
				}
			}
		}
	}
	return problems
}

func TestReadmeCommandProblems(t *testing.T) {
	world := commandWorld{
		isDir: func(dir string) bool {
			return slices.Contains([]string{".", "examples", "examples/app", "examples/lib"}, dir)
		},
		mainFlags: func(dir string) ([]string, bool) {
			if dir == "examples/app" {
				return []string{"check", "issue-token"}, true
			}
			return nil, false
		},
	}
	tests := []struct {
		name, dir, readme string
		want              []string
	}{
		{"clone-relative", "examples/app", "```sh\ngit clone https://github.com/angvp/tango\ncd tango/examples/app\ngo run . -check\ngo run . -issue-token=ada\n```", nil},
		{"from the root", ".", "```sh\n$ cd examples/app && go run .\n# a comment\ncurl localhost:8000/\n```", nil},
		{"not a shell block", ".", "```go\ncd nowhere\n```", nil},
		{"a scaffolded project", ".", "```sh\ntango newproject shop\ncd shop\ngo run .\n```", nil},
		{"missing directory", ".", "```sh\ncd examples/gone\n```", []string{"cd examples/gone: no such directory"}},
		{"not a main package", ".", "```sh\ncd examples/lib\ngo run .\n```", []string{"go run . in examples/lib: not a main package"}},
		{"undeclared flag", "examples/app", "```sh\ngo run . -migrate\n```", []string{"go run . -migrate in examples/app: examples/app declares no such flag"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := readmeCommandProblems(tt.dir, tt.readme, world); !slices.Equal(got, tt.want) {
				t.Fatalf("problems = %q, want %q", got, tt.want)
			}
		})
	}
}

var (
	declaredFlag  = regexp.MustCompile(`flag\.\w+\("([^"]+)"`)
	dispatchFlag  = regexp.MustCompile(`flags\.\w+\("([^"]+)"`)
	adminFlag     = regexp.MustCompile(`"-(tango-admin-[a-z-]+)"`)
	mainPackage   = regexp.MustCompile(`(?m)^package main$`)
	readSourceDir = func(t *testing.T, dir string) string {
		t.Helper()
		files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
		var b strings.Builder
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			content, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			b.Write(content)
		}
		return b.String()
	}
)

func matches(re *regexp.Regexp, text string) []string {
	var out []string
	for _, m := range re.FindAllStringSubmatch(text, -1) {
		out = append(out, m[1])
	}
	return out
}

// TestReadmeCommandsMatchTheRepository checks the root README's and every
// example README's commands against the repository, and that the root
// README lists every example.
func TestReadmeCommandsMatchTheRepository(t *testing.T) {
	dispatchFlags := matches(dispatchFlag, readSourceDir(t, "."))
	adminFlags := matches(adminFlag, readSourceDir(t, "admin"))
	world := commandWorld{
		isDir: func(dir string) bool {
			info, err := os.Stat(dir)
			return err == nil && info.IsDir()
		},
		mainFlags: func(dir string) ([]string, bool) {
			source := readSourceDir(t, dir)
			flags := matches(declaredFlag, source)
			if strings.Contains(source, "tango.DispatchFlags(") {
				flags = append(flags, dispatchFlags...)
			}
			if strings.Contains(source, "admin.HandleCLI(") {
				flags = append(flags, adminFlags...)
			}
			return flags, mainPackage.MatchString(source)
		},
	}

	readmes, _ := filepath.Glob("examples/*/README.md")
	readmes = append([]string{"README.md"}, readmes...)
	for _, readme := range readmes {
		text, err := os.ReadFile(readme)
		if err != nil {
			t.Fatal(err)
		}
		for _, problem := range readmeCommandProblems(filepath.ToSlash(filepath.Dir(readme)), string(text), world) {
			t.Errorf("%s: %s", readme, problem)
		}
	}

	root, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	examples, _ := os.ReadDir("examples")
	for _, example := range examples {
		if example.IsDir() && !strings.Contains(string(root), "examples/"+example.Name()) {
			t.Errorf("README.md doesn't list examples/%s", example.Name())
		}
		if example.IsDir() {
			if _, err := os.Stat(filepath.Join("examples", example.Name(), "README.md")); err != nil {
				t.Errorf("examples/%s has no README", example.Name())
			}
		}
	}
}

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

// shellSession follows one shell block's commands: where they run, whether
// this repository was cloned, and which directories they created.
type shellSession struct {
	world   commandWorld
	cwd     string
	cloned  bool
	created map[string]bool
}

// readmeCommandProblems checks the shell blocks of a README in dir: every
// cd leads to a directory, every `go run .` runs a main package, and every
// flag passed to it is one that binary declares. A `git clone` of this
// repository makes `cd tango/…` relative to the repository root, and a
// directory `tango newproject` or `mkdir` creates counts as existing, its
// main package trusted to the scaffolder. Commands aren't run.
func readmeCommandProblems(dir, text string, world commandWorld) []string {
	var problems []string
	var session *shellSession
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case session == nil && shellFence.MatchString(trimmed):
			session = &shellSession{world: world, cwd: dir, created: map[string]bool{}}
		case session != nil && trimmed == "```":
			session = nil
		case session != nil:
			for _, command := range strings.Split(strings.TrimPrefix(trimmed, "$ "), "&&") {
				problems = append(problems, session.run(strings.Fields(command))...)
			}
		}
	}
	return problems
}

// run follows one command, returning what's wrong with it.
func (s *shellSession) run(fields []string) []string {
	switch {
	case len(fields) == 0 || strings.HasPrefix(fields[0], "#"):
	case len(fields) >= 3 && fields[0] == "git" && fields[1] == "clone" && strings.TrimSuffix(fields[2], ".git") == "https://github.com/angvp/tango":
		s.cloned = true
	case len(fields) >= 3 && fields[0] == "tango" && fields[1] == "newproject":
		s.created[path.Join(s.cwd, fields[len(fields)-1])] = true
	case len(fields) == 2 && fields[0] == "mkdir":
		s.created[path.Join(s.cwd, fields[1])] = true
	case len(fields) == 2 && fields[0] == "cd":
		return s.cd(fields[1])
	case len(fields) >= 3 && fields[0] == "go" && fields[1] == "run" && fields[2] == ".":
		return s.goRun(fields[3:])
	}
	return nil
}

// cd moves to dir, which must exist or have been created.
func (s *shellSession) cd(dir string) []string {
	target := path.Join(s.cwd, dir)
	if s.cloned && (dir == "tango" || strings.HasPrefix(dir, "tango/")) {
		target = path.Clean(strings.TrimPrefix(strings.TrimPrefix(dir, "tango"), "/"))
	}
	if !s.world.isDir(target) && !s.created[target] {
		return []string{"cd " + dir + ": no such directory"}
	}
	s.cwd = target
	return nil
}

// goRun checks `go run .` with args in the current directory.
func (s *shellSession) goRun(args []string) []string {
	if s.created[s.cwd] {
		return nil
	}
	flags, isMain := s.world.mainFlags(s.cwd)
	if !isMain {
		return []string{"go run . in " + s.cwd + ": not a main package"}
	}
	var problems []string
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			continue
		}
		if name, _, _ := strings.Cut(strings.TrimLeft(arg, "-"), "="); !slices.Contains(flags, name) {
			problems = append(problems, "go run . "+arg+" in "+s.cwd+": "+s.cwd+" declares no such flag")
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
	declaredFlag = regexp.MustCompile(`flag\.\w+\("([^"]+)"`)
	dispatchFlag = regexp.MustCompile(`flags\.\w+\("([^"]+)"`)
	adminFlag    = regexp.MustCompile(`"-(tango-admin-[a-z-]+)"`)
	mainPackage  = regexp.MustCompile(`(?m)^package main$`)
)

// readSource returns the non-test Go source in dir, concatenated.
func readSource(t *testing.T, dir string) string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
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

func matches(re *regexp.Regexp, text string) []string {
	var out []string
	for _, m := range re.FindAllStringSubmatch(text, -1) {
		out = append(out, m[1])
	}
	return out
}

// repositoryWorld is the repository as README commands see it, with the
// flags DispatchFlags and the admin CLI add read from their source.
func repositoryWorld(t *testing.T) commandWorld {
	dispatchFlags := matches(dispatchFlag, readSource(t, "."))
	adminFlags := matches(adminFlag, readSource(t, "admin"))
	return commandWorld{
		isDir: func(dir string) bool {
			info, err := os.Stat(dir)
			return err == nil && info.IsDir()
		},
		mainFlags: func(dir string) ([]string, bool) {
			source := readSource(t, dir)
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
}

// TestReadmeCommandsMatchTheRepository checks the root README's and every
// example README's commands against the repository.
func TestReadmeCommandsMatchTheRepository(t *testing.T) {
	world := repositoryWorld(t)
	readmes, err := filepath.Glob("examples/*/README.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, readme := range append([]string{"README.md"}, readmes...) {
		text, err := os.ReadFile(readme)
		if err != nil {
			t.Fatal(err)
		}
		for _, problem := range readmeCommandProblems(filepath.ToSlash(filepath.Dir(readme)), string(text), world) {
			t.Errorf("%s: %s", readme, problem)
		}
	}
}

// TestTheReadmeListsEveryExample keeps the root README's examples list,
// and the examples' own READMEs, complete.
func TestTheReadmeListsEveryExample(t *testing.T) {
	root, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	examples, err := os.ReadDir("examples")
	if err != nil {
		t.Fatal(err)
	}
	if len(examples) == 0 {
		t.Fatal("no examples found")
	}
	for _, example := range examples {
		if !example.IsDir() {
			continue
		}
		if !strings.Contains(string(root), "](examples/"+example.Name()+")") {
			t.Errorf("README.md doesn't link examples/%s", example.Name())
		}
		if _, err := os.Stat(filepath.Join("examples", example.Name(), "README.md")); err != nil {
			t.Errorf("examples/%s has no README", example.Name())
		}
	}
}

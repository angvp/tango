package shellcore

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/internal/migrationtest"
	"github.com/angvp/tango/testdb"
)

type Post struct {
	ID        int64 `tango:"pk"`
	Title     string
	Published bool
	Views     int
	Score     float64
	PostedAt  time.Time
}

type Comment struct {
	ID     int64 `tango:"pk"`
	PostID int64 `tango:"fk=Post"`
	Body   string
}

// bootProject registers a blog app with Post and Comment in a fresh database
// and returns the booted pieces a session works on.
func bootProject(t *testing.T) Boot {
	t.Helper()
	blog := tango.NewApp("blog", func(r *tango.Registry) error {
		if err := r.Models().Register(Post{}); err != nil {
			return err
		}
		return r.Models().Register(Comment{})
	})
	registry, err := tango.BuildRegistry(tango.Config{InstalledApps: []tango.App{blog}})
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatal(err)
	}
	sqlDB, dialect := testdb.Open(t)
	migrationtest.Apply(t, sqlDB, dialect, registry.Models().All())
	store := db.NewStore(sqlDB, dialect)
	registry.SetStore(store)
	return Boot{Registry: registry, Store: store}
}

func runProject(t *testing.T, boot Boot, stdin string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = Run(context.Background(), IO{In: strings.NewReader(stdin), Out: &out, Err: &errOut}, boot, args)
	return code, out.String(), errOut.String()
}

func newVerbs(t *testing.T, readOnly bool) *Verbs {
	t.Helper()
	boot := bootProject(t)
	return NewVerbs(context.Background(), boot.Registry.Models(), boot.Store, readOnly)
}

func TestModelsAreQualifiedAndSorted(t *testing.T) {
	got := newVerbs(t, false).Models()
	if want := "blog.Comment blog.Post"; strings.Join(got, " ") != want {
		t.Fatalf("Models() = %v, want %s", got, want)
	}
}

func TestDescribeListsEachFieldOnceWithItsKeyFlags(t *testing.T) {
	rows, err := newVerbs(t, false).Describe("blog.Comment")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[0]["Name"] != "ID" || rows[0]["PrimaryKey"] != true {
		t.Fatalf("rows = %v", rows)
	}
	if rows[1]["Name"] != "PostID" || rows[1]["ForeignKey"] != "blog.Post" || rows[1]["Type"] != "int64" {
		t.Fatalf("PostID row = %v, want a foreign key to blog.Post", rows[1])
	}
}

func TestCreateGetUpdateListCountDeleteRoundTrip(t *testing.T) {
	v := newVerbs(t, false)
	created, err := v.Create("blog.Post", map[string]any{"Title": "Hello", "Published": true, "Views": 3, "Score": 1.5})
	if err != nil {
		t.Fatal(err)
	}
	id := created["ID"]
	if id == nil || id.(int64) < 1 || created["Title"] != "Hello" || created["Views"] != 3 {
		t.Fatalf("created = %v, want the stored row with its assigned ID and typed values", created)
	}
	if _, err := v.Create("blog.Post", map[string]any{"Title": "Draft"}); err != nil {
		t.Fatal(err)
	}

	got, err := v.Get("blog.Post", 1)
	if err != nil || got["Title"] != "Hello" {
		t.Fatalf("Get = %v, %v", got, err)
	}
	if _, ok := got["PostedAt"].(time.Time); !ok {
		t.Fatalf("row values keep their Go types, PostedAt is %T", got["PostedAt"])
	}

	updated, err := v.Update("blog.Post", 1, map[string]any{"Title": "Hello again", "Views": 4})
	if err != nil || updated["Title"] != "Hello again" || updated["Views"] != 4 || updated["Published"] != true {
		t.Fatalf("Update = %v, %v, want the changed fields and the rest kept", updated, err)
	}

	all, err := v.List("blog.Post")
	if err != nil || len(all) != 2 {
		t.Fatalf("List = %v, %v", all, err)
	}
	published, err := v.List("blog.Post", map[string]any{"where": map[string]any{"Published": true}})
	if err != nil || len(published) != 1 || published[0]["Title"] != "Hello again" {
		t.Fatalf("List where = %v, %v", published, err)
	}
	ordered, err := v.List("blog.Post", map[string]any{"order": []string{"-ID"}, "limit": 1})
	if err != nil || len(ordered) != 1 || ordered[0]["Title"] != "Draft" {
		t.Fatalf("List order/limit = %v, %v", ordered, err)
	}
	skipped, err := v.List("blog.Post", map[string]any{"order": []string{"ID"}, "limit": 5, "offset": 1})
	if err != nil || len(skipped) != 1 || skipped[0]["Title"] != "Draft" {
		t.Fatalf("List offset = %v, %v", skipped, err)
	}
	if n, err := v.Count("blog.Post", map[string]any{"where": map[string]any{"Views": 4}}); err != nil || n != 1 {
		t.Fatalf("Count = %d, %v, want 1", n, err)
	}
	if n, err := v.Count("blog.Post"); err != nil || n != 2 {
		t.Fatalf("Count all = %d, %v", n, err)
	}

	if err := v.Delete("blog.Post", 2); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Get("blog.Post", 2); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("Get after Delete = %v, want ErrNotFound", err)
	}
}

func TestForeignKeysAreCheckedByTheStore(t *testing.T) {
	v := newVerbs(t, false)
	if _, err := v.Create("blog.Comment", map[string]any{"PostID": 99, "Body": "orphan"}); !errors.Is(err, db.ErrInvalidForeignKey) {
		t.Fatalf("Create = %v, want ErrInvalidForeignKey", err)
	}
}

func TestBadInputIsAnErrorThatNamesTheProblem(t *testing.T) {
	v := newVerbs(t, false)
	tests := []struct {
		name string
		call func() error
		want string
	}{
		{"bare model", func() error { _, err := v.List("Post"); return err }, `did you mean blog.Post`},
		{"unknown model", func() error { _, err := v.Get("blog.Nope", 1); return err }, `unknown model "blog.Nope"`},
		{"wrong app", func() error { _, err := v.Get("shop.Post", 1); return err }, `unknown model "shop.Post"`},
		{"unknown where field", func() error {
			_, err := v.List("blog.Post", map[string]any{"where": map[string]any{"Nope": 1}})
			return err
		}, `has no field "Nope" (fields: ID, Title`},
		{"bad where value type", func() error {
			_, err := v.List("blog.Post", map[string]any{"where": map[string]any{"Views": "many"}})
			return err
		}, "want int, got string"},
		{"bad order field", func() error {
			_, err := v.List("blog.Post", map[string]any{"order": []string{"Nope"}})
			return err
		}, `unknown OrderBy field "Nope"`},
		{"order is not a list", func() error { _, err := v.List("blog.Post", map[string]any{"order": "ID"}); return err }, `"order"`},
		{"unknown query key", func() error { _, err := v.List("blog.Post", map[string]any{"filter": 1}); return err }, `unknown query key "filter"`},
		{"negative limit", func() error { _, err := v.List("blog.Post", map[string]any{"limit": -1}); return err }, `"limit"`},
		{"two query maps", func() error { _, err := v.List("blog.Post", map[string]any{}, map[string]any{}); return err }, "one query map"},
		{"unknown create field", func() error { _, err := v.Create("blog.Post", map[string]any{"Nope": 1}); return err }, `has no field "Nope"`},
		{"wrong create type", func() error { _, err := v.Create("blog.Post", map[string]any{"Views": "x"}); return err }, "field Views: want int, got string"},
		{"fractional into int", func() error { _, err := v.Create("blog.Post", map[string]any{"Views": 1.5}); return err }, "does not fit int"},
		{"nil into string", func() error { _, err := v.Create("blog.Post", map[string]any{"Title": nil}); return err }, "cannot be nil"},
		{"missing row", func() error { _, err := v.Get("blog.Post", 42); return err }, "not found"},
		{"primary key of the wrong type", func() error { _, err := v.Get("blog.Post", "one"); return err }, "primary key ID"},
		{"update changes the primary key", func() error { _, err := v.Update("blog.Post", 1, map[string]any{"ID": 5}); return err }, "primary key ID cannot be changed"},
		{"update missing row", func() error { _, err := v.Update("blog.Post", 42, map[string]any{"Title": "x"}); return err }, "not found"},
		{"delete missing row", func() error { return v.Delete("blog.Post", 42) }, "not found"},
	}
	if _, err := v.Create("blog.Post", map[string]any{"Title": "one"}); err != nil {
		t.Fatal(err)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want one containing %q", err, tt.want)
			}
		})
	}
}

func TestReadOnlyRefusesEveryWriteAndStillReads(t *testing.T) {
	boot := bootProject(t)
	writable := NewVerbs(context.Background(), boot.Registry.Models(), boot.Store, false)
	if _, err := writable.Create("blog.Post", map[string]any{"Title": "kept"}); err != nil {
		t.Fatal(err)
	}
	v := NewVerbs(context.Background(), boot.Registry.Models(), boot.Store, true)

	if _, err := v.Create("blog.Post", map[string]any{"Title": "x"}); !errors.Is(err, ErrReadOnly) || !strings.Contains(err.Error(), "--readonly") {
		t.Errorf("Create = %v, want a read-only refusal naming the flag", err)
	}
	if _, err := v.Update("blog.Post", 1, map[string]any{"Title": "x"}); !errors.Is(err, ErrReadOnly) {
		t.Errorf("Update = %v, want ErrReadOnly", err)
	}
	if err := v.Delete("blog.Post", 1); !errors.Is(err, ErrReadOnly) {
		t.Errorf("Delete = %v, want ErrReadOnly", err)
	}
	if rows, err := v.List("blog.Post"); err != nil || len(rows) != 1 || rows[0]["Title"] != "kept" {
		t.Errorf("List = %v, %v, want the untouched row", rows, err)
	}
	if n, err := v.Count("blog.Post"); err != nil || n != 1 {
		t.Errorf("Count = %d, %v", n, err)
	}
}

func TestContextIsTheSessionContext(t *testing.T) {
	boot := bootProject(t)
	type key struct{}
	ctx := context.WithValue(context.Background(), key{}, "set")
	v := NewVerbs(ctx, boot.Registry.Models(), boot.Store, false)
	if v.Context().Value(key{}) != "set" {
		t.Fatal("Context() is not the context the session was started with")
	}
}

// The verbs as a person types them in a session, through the interpreter.
func TestVerbsWorkFromTheInterpreter(t *testing.T) {
	boot := bootProject(t)
	script := strings.Join([]string{
		`Models()`,
		`Create("blog.Post", map[string]any{"Title": "From the shell", "Published": true})`,
		`p := Get("blog.Post", 1)`,
		`Count("blog.Post")`,
		`List("blog.Post", map[string]any{"where": map[string]any{"Published": true}, "order": []string{"-ID"}, "limit": 10})`,
		`Update("blog.Post", 1, map[string]any{"Views": 9})`,
		`rows, err := List("blog.Post")`,
		`len(rows)`,
		`Delete("blog.Post", 1)`,
		`Count("blog.Post")`,
		`Get("blog.Post", 1)`,
		`Count("blog.Post")`,
	}, "\n")
	code, out, errOut := runProject(t, boot, script)
	if code != 1 || !strings.Contains(errOut, "error: get blog.Post: tango db: not found") {
		t.Fatalf("code=%d err=%q, want the failing Get to stop the run", code, errOut)
	}
	row := func(views string) string {
		return `map[ID:1 PostedAt:0001-01-01T00:00:00Z Published:true Score:0 Title:"From the shell" Views:` + views + `]`
	}
	want := strings.Join([]string{
		`["blog.Comment" "blog.Post"]`, // Models()
		row("0"),                       // Create
		"1",                            // Count
		row("0"),                       // List with where, order, limit
		row("9"),                       // Update
		"1",                            // len(rows)
		"0",                            // Count after Delete
	}, "\n") + "\n"
	if out != want {
		t.Fatalf("stdout:\n%s\nwant:\n%s", out, want)
	}
}

func TestReadOnlyFlagRefusesWritesThroughTheCommandLine(t *testing.T) {
	boot := bootProject(t)
	code, out, errOut := runProject(t, boot, "", "--readonly", "-c", `Create("blog.Post", map[string]any{"Title": "x"})`)
	if code != 1 || out != "" || !strings.Contains(errOut, "error: Create refused: the shell is read-only (--readonly)") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
	if n, err := NewVerbs(context.Background(), boot.Registry.Models(), boot.Store, false).Count("blog.Post"); err != nil || n != 0 {
		t.Fatalf("Count = %d, %v, want nothing written", n, err)
	}
}

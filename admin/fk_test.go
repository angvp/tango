package admin_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/angvp/tango"
	"github.com/angvp/tango/admin"
	"github.com/angvp/tango/db"
	_ "modernc.org/sqlite"
)

type fkAuthor struct {
	ID   int64 `tango:"pk"`
	Name string
}

type fkPost struct {
	ID       int64 `tango:"pk"`
	Title    string
	AuthorID int64 `tango:"fk=fkAuthor"`
}

// buildAuthorPostAdmin wires an Author/Post pair through admin, mirroring
// buildProductAdminWithOptions in crud_test.go. authorOptions lets a test
// omit Label to exercise the raw-PK fallback.
func buildAuthorPostAdmin(t *testing.T, authorOptions admin.Options) (http.Handler, *sql.DB) {
	t.Helper()

	registry := tango.NewRegistry()
	if err := registry.Models().Register(fkAuthor{}); err != nil {
		t.Fatalf("register fkAuthor: %v", err)
	}
	if err := registry.Admin().Register(fkAuthor{}, authorOptions); err != nil {
		t.Fatalf("register admin fkAuthor: %v", err)
	}
	if err := registry.Models().Register(fkPost{}); err != nil {
		t.Fatalf("register fkPost: %v", err)
	}
	if err := registry.Admin().Register(fkPost{}, admin.Options{
		ListDisplay: []string{"Title", "AuthorID"},
	}); err != nil {
		t.Fatalf("register admin fkPost: %v", err)
	}

	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	for _, stmt := range []string{
		`CREATE TABLE fk_author (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL)`,
		`CREATE TABLE fk_post (id INTEGER PRIMARY KEY AUTOINCREMENT, title TEXT NOT NULL, author_id INTEGER NOT NULL)`,
		`CREATE TABLE admin_user (id INTEGER PRIMARY KEY AUTOINCREMENT, username TEXT NOT NULL UNIQUE, password_hash TEXT NOT NULL, active BOOLEAN NOT NULL, created_at TIMESTAMP NOT NULL)`,
		`CREATE TABLE admin_session (id INTEGER PRIMARY KEY AUTOINCREMENT, token TEXT NOT NULL UNIQUE, user_id INTEGER NOT NULL, expires_at TIMESTAMP NOT NULL)`,
	} {
		if _, err := sqlDB.Exec(stmt); err != nil {
			t.Fatalf("create table: %v", err)
		}
	}

	store := db.NewStore(sqlDB, db.SQLite)
	if err := admin.CreateAccount(context.Background(), store, "admin", "secret"); err != nil {
		t.Fatalf("seed admin account: %v", err)
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO admin_session (token, user_id, expires_at) SELECT ?, id, ? FROM admin_user WHERE username = ?`,
		testSessionToken, time.Now().Add(time.Hour).UTC(), "admin",
	); err != nil {
		t.Fatalf("seed admin session: %v", err)
	}
	if err := registry.Register(admin.New(store)); err != nil {
		t.Fatalf("register admin app: %v", err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatalf("run registration: %v", err)
	}

	handler, err := registry.Routes().Handler()
	if err != nil {
		t.Fatalf("build handler: %v", err)
	}
	return handler, sqlDB
}

func seedAuthor(t *testing.T, sqlDB *sql.DB, name string) int64 {
	t.Helper()
	result, err := sqlDB.Exec(`INSERT INTO fk_author (name) VALUES (?)`, name)
	if err != nil {
		t.Fatalf("seed author: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("seed author id: %v", err)
	}
	return id
}

func seedPost(t *testing.T, sqlDB *sql.DB, title string, authorID int64) int64 {
	t.Helper()
	result, err := sqlDB.Exec(`INSERT INTO fk_post (title, author_id) VALUES (?, ?)`, title, authorID)
	if err != nil {
		t.Fatalf("seed post: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("seed post id: %v", err)
	}
	return id
}

func TestCreateViewRendersForeignKeyAsSelectWithRelatedLabel(t *testing.T) {
	handler, sqlDB := buildAuthorPostAdmin(t, admin.Options{Label: "Name"})
	authorID := seedAuthor(t, sqlDB, "Jane Doe")

	response := doRequest(t, handler, http.MethodGet, "/admin/fk_post/new/", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}

	body := response.Body.String()
	if !strings.Contains(body, `<select`) {
		t.Fatalf("body does not contain a <select> for the foreign key field:\n%s", body)
	}
	wantOption := `value="` + itoa(authorID) + `"`
	if !strings.Contains(body, wantOption) || !strings.Contains(body, "Jane Doe") {
		t.Fatalf("body does not contain the author's option (value=%d, label %q):\n%s", authorID, "Jane Doe", body)
	}
}

func TestCreateViewSubmittingForeignKeySelectCreatesRow(t *testing.T) {
	handler, sqlDB := buildAuthorPostAdmin(t, admin.Options{Label: "Name"})
	authorID := seedAuthor(t, sqlDB, "Jane Doe")

	response := doRequest(t, handler, http.MethodPost, "/admin/fk_post/new/", url.Values{
		"Title":    {"Hello World"},
		"AuthorID": {itoa(authorID)},
	})
	if response.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302, body:\n%s", response.Code, response.Body.String())
	}

	var gotAuthorID int64
	if err := sqlDB.QueryRow(`SELECT author_id FROM fk_post WHERE title = ?`, "Hello World").Scan(&gotAuthorID); err != nil {
		t.Fatalf("query created post: %v", err)
	}
	if gotAuthorID != authorID {
		t.Fatalf("author_id = %d, want %d", gotAuthorID, authorID)
	}
}

func TestEditViewSubmittingForeignKeySelectUpdatesRow(t *testing.T) {
	handler, sqlDB := buildAuthorPostAdmin(t, admin.Options{Label: "Name"})
	firstAuthorID := seedAuthor(t, sqlDB, "Jane Doe")
	secondAuthorID := seedAuthor(t, sqlDB, "John Smith")
	postID := seedPost(t, sqlDB, "Hello World", firstAuthorID)

	response := doRequest(t, handler, http.MethodPost, "/admin/fk_post/"+itoa(postID)+"/", url.Values{
		"Title":    {"Hello World"},
		"AuthorID": {itoa(secondAuthorID)},
	})
	if response.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302, body:\n%s", response.Code, response.Body.String())
	}

	var gotAuthorID int64
	if err := sqlDB.QueryRow(`SELECT author_id FROM fk_post WHERE id = ?`, postID).Scan(&gotAuthorID); err != nil {
		t.Fatalf("query updated post: %v", err)
	}
	if gotAuthorID != secondAuthorID {
		t.Fatalf("author_id = %d, want %d", gotAuthorID, secondAuthorID)
	}
}

func TestEditViewPreselectsCurrentForeignKeyValue(t *testing.T) {
	handler, sqlDB := buildAuthorPostAdmin(t, admin.Options{Label: "Name"})
	authorID := seedAuthor(t, sqlDB, "Jane Doe")
	postID := seedPost(t, sqlDB, "Hello World", authorID)

	response := doRequest(t, handler, http.MethodGet, "/admin/fk_post/"+itoa(postID)+"/", nil)
	body := response.Body.String()
	wantSelected := `value="` + itoa(authorID) + `" selected`
	if !strings.Contains(body, wantSelected) {
		t.Fatalf("body does not mark the current author as selected:\n%s", body)
	}
}

func TestListViewShowsRelatedLabelForForeignKeyColumn(t *testing.T) {
	handler, sqlDB := buildAuthorPostAdmin(t, admin.Options{Label: "Name"})
	authorID := seedAuthor(t, sqlDB, "Jane Doe")
	seedPost(t, sqlDB, "Hello World", authorID)

	response := doRequest(t, handler, http.MethodGet, "/admin/fk_post/", nil)
	body := response.Body.String()
	if !strings.Contains(body, "Jane Doe") {
		t.Fatalf("list page does not show the related author's label:\n%s", body)
	}
	if strings.Contains(body, ">"+itoa(authorID)+"<") {
		t.Fatalf("list page shows the raw author id instead of the label:\n%s", body)
	}
}

func TestListViewFallsBackToRawIDWithoutLabelConfigured(t *testing.T) {
	handler, sqlDB := buildAuthorPostAdmin(t, admin.Options{}) // no Label set
	authorID := seedAuthor(t, sqlDB, "Jane Doe")
	seedPost(t, sqlDB, "Hello World", authorID)

	response := doRequest(t, handler, http.MethodGet, "/admin/fk_post/", nil)
	body := response.Body.String()
	if strings.Contains(body, "Jane Doe") {
		t.Fatalf("list page should not show the label when Label is unset:\n%s", body)
	}
	if !strings.Contains(body, ">"+itoa(authorID)+"<") {
		t.Fatalf("list page should fall back to the raw author id when Label is unset:\n%s", body)
	}
}

func itoa(v int64) string {
	return strconv.FormatInt(v, 10)
}

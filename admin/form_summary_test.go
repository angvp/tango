package admin_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/admin"
)

type adminTally struct {
	ID    int64 `tango:"pk"`
	Title string
	Count int
	Total int
}

const tallyBasePath = "/admin/admin_tally/"

func buildTallyAdmin(t *testing.T, options admin.Options) http.Handler {
	t.Helper()
	registry := tango.NewRegistry()
	if err := registry.Models().Register(adminTally{}); err != nil {
		t.Fatalf("register model: %v", err)
	}
	if err := registry.Admin().Register(adminTally{}, options); err != nil {
		t.Fatalf("register admin model: %v", err)
	}
	sqlDB, store := migratedAdminDB(t, registry)
	seedAdminAccountAndSession(t, sqlDB, store)
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
	return handler
}

func TestFailedSaveWithTwoProblemsShowsASummaryAboveTheFields(t *testing.T) {
	handler := buildTallyAdmin(t, admin.Options{SingularName: "Tally"})
	body := doRequest(t, handler, http.MethodPost, tallyBasePath+"new/", url.Values{"Count": {"x"}, "Total": {"y"}}).Body.String()

	summary := strings.Index(body, `role="alert"`)
	firstField := strings.Index(body, `id="field-Title"`)
	if summary < 0 || firstField < 0 || summary > firstField {
		t.Fatalf("want a role=alert summary above the first field:\n%s", body)
	}
	if !strings.Contains(body, "Fix 2 problems to save this tally.") {
		t.Errorf("summary does not count the problems in words:\n%s", body)
	}
	for _, name := range []string{"Count", "Total"} {
		if !strings.Contains(body, `<li><a href="#field-`+name+`" class="underline">`+name+`: `+name+` must be a whole number.</a></li>`) {
			t.Errorf("summary has no link to field %s carrying its message once:\n%s", name, body)
		}
	}
	if got := strings.Count(body, `role="alert"`); got != 1 {
		t.Errorf("want role=alert on the summary only, found %d:\n%s", got, body)
	}
	if got := strings.Count(body, "autofocus"); got != 1 || !strings.Contains(body, `tabindex="-1" autofocus`) {
		t.Errorf("want one autofocus on the summary heading, found %d:\n%s", got, body)
	}
}

func TestFailedSaveWithOneFieldProblemShowsNoSummary(t *testing.T) {
	handler := buildTallyAdmin(t, admin.Options{})
	body := doRequest(t, handler, http.MethodPost, tallyBasePath+"new/", url.Values{"Count": {"x"}}).Body.String()

	if strings.Contains(body, `href="#field-`) || strings.Contains(body, "Fix ") {
		t.Errorf("a single field problem should show only its field message:\n%s", body)
	}
	if !strings.Contains(body, `id="field-Count-error" role="alert"`) {
		t.Errorf("the field message is missing or not announced:\n%s", body)
	}
}

func TestHelpTextSitsBetweenTheLabelAndItsControl(t *testing.T) {
	handler := buildNoteAdmin(t, admin.Options{HelpText: map[string]string{"Title": "Keep it short."}})
	body := doRequest(t, handler, http.MethodGet, noteBasePath+"new/", nil).Body.String()

	label := strings.Index(body, `<label for="field-Title"`)
	help := strings.Index(body, `id="field-Title-help"`)
	control := strings.Index(body, `<input id="field-Title"`)
	if label < 0 || help < 0 || control < 0 || !(label < help && help < control) {
		t.Errorf("want label, then help, then control:\n%s", body)
	}
}

func TestFormHasNoRequiredMarker(t *testing.T) {
	handler := buildNoteAdmin(t, admin.Options{})
	body := doRequest(t, handler, http.MethodGet, noteBasePath+"new/", nil).Body.String()

	if strings.Contains(body, " required") || strings.Contains(body, "Required") {
		t.Errorf("no field may be marked required:\n%s", body)
	}
}

func TestCancelNamesItsDestination(t *testing.T) {
	handler := buildNoteAdmin(t, admin.Options{PluralName: "Notes"})
	body := doRequest(t, handler, http.MethodGet, noteBasePath+"new/", nil).Body.String()

	if !strings.Contains(body, `href="../" class="btn-secondary max-sm:w-full max-sm:whitespace-normal max-sm:text-center">Cancel and return to notes</a>`) {
		t.Errorf("cancel does not name the list it returns to:\n%s", body)
	}
}

func TestForeignKeyEmptyChoiceNamesTheTargetNoun(t *testing.T) {
	handler, sqlDB := buildAuthorPostAdmin(t, admin.Options{Label: "Name", SingularName: "Author"})
	seedAuthor(t, sqlDB, "Jane Doe")

	body := doRequest(t, handler, http.MethodGet, "/admin/fk_post/new/", nil).Body.String()
	if !strings.Contains(body, `<option value="" selected>No author selected</option>`) {
		t.Errorf("empty choice does not name the author:\n%s", body)
	}
}

func TestForeignKeyAddButtonHasVisibleTargetName(t *testing.T) {
	handler, _ := buildAuthorPostAdmin(t, admin.Options{Label: "Name", SingularName: "Author"})

	body := doRequest(t, handler, http.MethodGet, "/admin/fk_post/new/", nil).Body.String()
	if !strings.Contains(body, `<span aria-hidden="true">+</span> Add new author</a>`) {
		t.Errorf("add button has no visible name for the target:\n%s", body)
	}
	if strings.Contains(body, `aria-label="Add`) {
		t.Errorf("add button still carries a bare aria-label:\n%s", body)
	}
}

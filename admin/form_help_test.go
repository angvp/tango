package admin_test

import (
	"context"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/admin"
)

// delegatingWidget is a host widget that hands its markup to a built-in one.
type delegatingWidget struct{ inner admin.Widget }

func (w delegatingWidget) Render(ctx context.Context, f admin.FieldContext) template.HTML {
	return w.inner.Render(ctx, f)
}

func (w delegatingWidget) Parse(ctx context.Context, f admin.FieldContext, form admin.FieldValues, dest reflect.Value) error {
	return w.inner.Parse(ctx, f, form, dest)
}

func TestDelegatingHostWidgetPrintsItsHelpOnce(t *testing.T) {
	handler := buildNoteAdmin(t, admin.Options{
		HelpText: map[string]string{"Body": "Keep it short."},
		Widgets:  map[string]admin.Widget{"Body": delegatingWidget{inner: admin.Textarea()}},
	})
	body := doRequest(t, handler, http.MethodGet, noteBasePath+"new/", nil).Body.String()

	if got := strings.Count(body, `id="field-Body-help"`); got != 1 {
		t.Errorf("help element printed %d times, want once:\n%s", got, body)
	}
}

func TestHostWidgetWithoutItsOwnHelpGetsItBeneathTheControl(t *testing.T) {
	handler := buildNoteAdmin(t, admin.Options{
		HelpText: map[string]string{"Title": "Keep it short."},
		Widgets:  map[string]admin.Widget{"Title": failingWidget{}},
	})
	body := doRequest(t, handler, http.MethodGet, noteBasePath+"new/", nil).Body.String()

	control := strings.Index(body, `<input id="field-Title"`)
	help := strings.Index(body, `id="field-Title-help"`)
	if control < 0 || help < 0 || help < control {
		t.Errorf("want a host widget's help after its control:\n%s", body)
	}
	if got := strings.Count(body, `id="field-Title-help"`); got != 1 {
		t.Errorf("help element printed %d times, want once:\n%s", got, body)
	}
}

func TestHelpThenErrorAreWiredToTheControl(t *testing.T) {
	handler := buildTallyAdmin(t, admin.Options{SingularName: "Tally", HelpText: map[string]string{"Count": "Whole copies."}})
	body := doRequest(t, handler, http.MethodPost, tallyBasePath+"new/", url.Values{"Count": {"x"}}).Body.String()

	if !strings.Contains(body, `<label for="field-Count" class="field-label">Count</label>`) {
		t.Errorf("label is not bound to the control:\n%s", body)
	}
	if !strings.Contains(body, `aria-describedby="field-Count-help field-Count-error"`) {
		t.Errorf("control is not described by help then error:\n%s", body)
	}
	label := strings.Index(body, `<label for="field-Count"`)
	help := strings.Index(body, `id="field-Count-help"`)
	control := strings.Index(body, `<input id="field-Count"`)
	if !(label < help && help < control) {
		t.Errorf("want label, help, control in that order:\n%s", body)
	}
}

func TestSingleFormLevelProblemShowsTheSummary(t *testing.T) {
	handler, _ := buildAuthorPostAdmin(t, admin.Options{Label: "Name", SingularName: "Author"})
	body := doRequest(t, handler, http.MethodPost, "/admin/fk_post/new/", url.Values{"Title": {"Hello"}, "AuthorID": {"999"}}).Body.String()

	if !strings.Contains(body, "Fix 1 problem to save this fk post.") {
		t.Errorf("a single form-level problem has no summary:\n%s", body)
	}
	if !strings.Contains(body, `tabindex="-1" autofocus`) {
		t.Errorf("summary heading does not take focus:\n%s", body)
	}
}

func TestFieldProblemFallbackTextNamesItsField(t *testing.T) {
	handler := buildNoteAdmin(t, admin.Options{
		Widgets: map[string]admin.Widget{
			"Title": failingWidget{err: errors.New("boom")},
			"Body":  failingWidget{err: errors.New("boom")},
		},
	})
	body := doRequest(t, handler, http.MethodPost, noteBasePath+"new/", url.Values{}).Body.String()

	for _, want := range []string{`<li><a href="#field-Title" class="underline">Title: Invalid value</a></li>`, `<li><a href="#field-Body" class="underline">Body: Invalid value</a></li>`} {
		if !strings.Contains(body, want) {
			t.Errorf("want %s:\n%s", want, body)
		}
	}
}

func TestSubmittedNumberIsKeptAfterAFailedSave(t *testing.T) {
	handler := buildTallyAdmin(t, admin.Options{SingularName: "Tally"})
	body := doRequest(t, handler, http.MethodPost, tallyBasePath+"new/", url.Values{"Count": {"x"}, "Total": {"7"}}).Body.String()

	if !strings.Contains(body, `id="field-Total" type="number" name="Total" value="7"`) {
		t.Errorf("the other number was not kept:\n%s", body)
	}
	if !strings.Contains(body, `name="Count" value="x"`) {
		t.Errorf("the refused number was not kept:\n%s", body)
	}
}

type adminFlag struct {
	ID   int64 `tango:"pk"`
	Done bool
}

func TestCheckboxHelpFollowsItsLabel(t *testing.T) {
	registry := tango.NewRegistry()
	if err := registry.Models().Register(adminFlag{}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Admin().Register(adminFlag{}, admin.Options{HelpText: map[string]string{"Done": "Tick when finished."}}); err != nil {
		t.Fatal(err)
	}
	sqlDB, store := migratedAdminDB(t, registry)
	seedAdminAccountAndSession(t, sqlDB, store)
	if err := registry.Register(admin.New(store)); err != nil {
		t.Fatal(err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatal(err)
	}
	handler, err := registry.Routes().Handler()
	if err != nil {
		t.Fatal(err)
	}
	body := doRequest(t, handler, http.MethodGet, "/admin/admin_flag/new/", nil).Body.String()

	if !regexp.MustCompile(`</label>\s*<p class="field-help" id="field-Done-help">`).MatchString(body) {
		t.Errorf("checkbox help does not follow its label directly:\n%s", body)
	}
}

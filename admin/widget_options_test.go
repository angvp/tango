package admin_test

import (
	"html/template"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/angvp/tango/admin"
)

func TestCreateReadOnlyFieldIgnoresSubmittedDataAndStaysZero(t *testing.T) {
	handler, sqlDB := buildProductAdminWithOptions(t, admin.Options{
		ListDisplay: []string{"Name", "Price"},
		ReadOnly:    []string{"Name"},
	})

	response := doRequest(t, handler, "POST", crudBasePath+"new/", url.Values{
		"Name":  {"should be ignored"},
		"Price": {"9.99"},
	})
	if response.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d; body: %s", response.Code, http.StatusFound, response.Body.String())
	}

	if countProducts(t, sqlDB) != 1 {
		t.Fatalf("product count = %d, want 1", countProducts(t, sqlDB))
	}
	product := getProduct(t, sqlDB, 1)
	if product.Name != "" {
		t.Fatalf("Name = %q, want empty (read-only field must stay at its Go zero value on create)", product.Name)
	}
	if product.Price != 9.99 {
		t.Fatalf("Price = %v, want 9.99 (non-read-only field must still be parsed)", product.Price)
	}
}

func TestEditReadOnlyFieldKeepsExistingValueRegardlessOfSubmittedData(t *testing.T) {
	handler, sqlDB := buildProductAdminWithOptions(t, admin.Options{
		ListDisplay: []string{"Name", "Price"},
		ReadOnly:    []string{"Name"},
	})
	id := seedProduct(t, sqlDB, "Original Name", 5.00)

	response := doRequest(t, handler, "POST", crudBasePath+itoa(id)+"/", url.Values{
		"Name":  {"attempted overwrite"},
		"Price": {"7.50"},
	})
	if response.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d; body: %s", response.Code, http.StatusFound, response.Body.String())
	}

	product := getProduct(t, sqlDB, id)
	if product.Name != "Original Name" {
		t.Fatalf("Name = %q, want unchanged %q (read-only field must ignore submitted data on edit)", product.Name, "Original Name")
	}
	if product.Price != 7.50 {
		t.Fatalf("Price = %v, want 7.50 (non-read-only field must still be parsed)", product.Price)
	}
}

func TestFieldOrderReordersRenderedFields(t *testing.T) {
	handler, _ := buildProductAdminWithOptions(t, admin.Options{
		ListDisplay: []string{"Name", "Price"},
		FieldOrder:  []string{"Price", "Name"},
	})

	response := doRequest(t, handler, "GET", crudBasePath+"new/", nil)
	body := response.Body.String()

	priceIndex := strings.Index(body, `name="Price"`)
	nameIndex := strings.Index(body, `name="Name"`)
	if priceIndex == -1 || nameIndex == -1 {
		t.Fatalf("expected both fields to render:\n%s", body)
	}
	if priceIndex > nameIndex {
		t.Fatalf("Price rendered after Name, want Price first per FieldOrder:\n%s", body)
	}
}

// uppercaseWidget is a minimal custom Widget used to prove admin.Options.Widgets
// fully replaces a field's default rendering and parsing.
type uppercaseWidget struct{}

func (uppercaseWidget) Render(f admin.FieldContext) template.HTML {
	return template.HTML(`<input data-widget="uppercase" name="` + f.Name + `" value="` + f.Value + `" class="input">`)
}

func (uppercaseWidget) Parse(f admin.FieldContext, form admin.FieldValues, dest reflect.Value) error {
	dest.SetString(strings.ToUpper(form.Get(f.Name)))
	return nil
}

func TestCustomWidgetReplacesDefaultRenderingAndParsing(t *testing.T) {
	handler, sqlDB := buildProductAdminWithOptions(t, admin.Options{
		ListDisplay: []string{"Name", "Price"},
		Widgets:     map[string]admin.Widget{"Name": uppercaseWidget{}},
	})

	getResponse := doRequest(t, handler, "GET", crudBasePath+"new/", nil)
	if !strings.Contains(getResponse.Body.String(), `data-widget="uppercase"`) {
		t.Fatalf("expected the custom widget's markup in the create form:\n%s", getResponse.Body.String())
	}

	postResponse := doRequest(t, handler, "POST", crudBasePath+"new/", url.Values{
		"Name":  {"acme"},
		"Price": {"1.00"},
	})
	if postResponse.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d; body: %s", postResponse.Code, http.StatusFound, postResponse.Body.String())
	}

	product := getProduct(t, sqlDB, 1)
	if product.Name != "ACME" {
		t.Fatalf("Name = %q, want %q (custom widget's Parse must have run)", product.Name, "ACME")
	}
}

// contextCapturingWidget records the FieldContext its Parse was called
// with, so a test can assert Parse receives the same populated context
// Render would have — not just a bare field name (see ADR 0013 and the
// shared buildFieldContext helper in admin/fields.go).
type contextCapturingWidget struct {
	parsedWith *admin.FieldContext
}

func (contextCapturingWidget) Render(f admin.FieldContext) template.HTML {
	return template.HTML(`<input name="` + f.Name + `" value="` + f.Value + `" class="input">`)
}

func (w contextCapturingWidget) Parse(f admin.FieldContext, form admin.FieldValues, dest reflect.Value) error {
	*w.parsedWith = f
	dest.SetString(form.Get(f.Name))
	return nil
}

func TestWidgetParseReceivesTheSamePopulatedFieldContextAsRender(t *testing.T) {
	var parsedWith admin.FieldContext
	handler, sqlDB := buildProductAdminWithOptions(t, admin.Options{
		ListDisplay: []string{"Name", "Price"},
		Labels:      map[string]string{"Name": "Product name"},
		HelpText:    map[string]string{"Name": "Shown to customers."},
		Widgets:     map[string]admin.Widget{"Name": contextCapturingWidget{parsedWith: &parsedWith}},
	})

	postResponse := doRequest(t, handler, "POST", crudBasePath+"new/", url.Values{
		"Name":  {"acme"},
		"Price": {"1.00"},
	})
	if postResponse.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d; body: %s", postResponse.Code, http.StatusFound, postResponse.Body.String())
	}

	if parsedWith.Label != "Product name" {
		t.Fatalf("Parse's FieldContext.Label = %q, want %q — Parse must receive the same context Render would have, not just a bare field name", parsedWith.Label, "Product name")
	}
	if parsedWith.HelpText != "Shown to customers." {
		t.Fatalf("Parse's FieldContext.HelpText = %q, want %q", parsedWith.HelpText, "Shown to customers.")
	}

	if getProduct(t, sqlDB, 1).Name != "acme" {
		t.Fatalf("create with a custom widget carrying a full FieldContext should still succeed")
	}
}

func TestTextareaBuiltinWidgetRendersATextarea(t *testing.T) {
	handler, sqlDB := buildProductAdminWithOptions(t, admin.Options{
		ListDisplay: []string{"Name", "Price"},
		Widgets:     map[string]admin.Widget{"Name": admin.Textarea()},
	})

	getResponse := doRequest(t, handler, "GET", crudBasePath+"new/", nil)
	if !strings.Contains(getResponse.Body.String(), "<textarea") {
		t.Fatalf("expected a <textarea> for the field using admin.Textarea():\n%s", getResponse.Body.String())
	}

	postResponse := doRequest(t, handler, "POST", crudBasePath+"new/", url.Values{
		"Name":  {"multi\nline"},
		"Price": {"1.00"},
	})
	if postResponse.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d; body: %s", postResponse.Code, http.StatusFound, postResponse.Body.String())
	}
	product := getProduct(t, sqlDB, 1)
	if product.Name != "multi\nline" {
		t.Fatalf("Name = %q, want %q", product.Name, "multi\nline")
	}
}

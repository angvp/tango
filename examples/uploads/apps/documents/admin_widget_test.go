package documents

import (
	"context"
	"reflect"
	"testing"

	"github.com/angvp/tango/admin"
	"github.com/angvp/tango/i18n"
	"github.com/angvp/tango/i18n/i18ntest"
)

func TestStoredKeyWidgetMessagesFollowTheRequestContext(t *testing.T) {
	i18ntest.Isolate(t)
	i18n.RegisterCatalog("zz-docs", map[string]string{
		"uploads.document.use_upload_form": "los archivos se añaden con el formulario de subida",
	})
	var key string
	dest := reflect.ValueOf(&key).Elem()

	localised := i18n.WithLocale(context.Background(), "zz-docs")
	err := storedKeyWidget{}.Parse(localised, admin.FieldContext{Name: "Key"}, nil, dest)
	if fe, ok := err.(admin.FieldError); !ok || fe.Message != "los archivos se añaden con el formulario de subida" {
		t.Fatalf("Parse error = %v; want the catalog wording", err)
	}

	err = storedKeyWidget{}.Parse(context.Background(), admin.FieldContext{Name: "Key"}, nil, dest)
	if fe, ok := err.(admin.FieldError); !ok || fe.Message != "files are added with the upload form, not here" {
		t.Fatalf("Parse error = %v; want the English fallback", err)
	}
}

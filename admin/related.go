package admin

import (
	"context"
	"fmt"
	"reflect"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/internal/adminregistry"
	"github.com/angvp/tango/model"
)

// labelFieldFor returns the field name that admin.Options.Label configured
// for model targetName, or "" if unset or the model isn't admin-registered.
func labelFieldFor(adminReg *adminregistry.Registry, targetName string) string {
	registration, ok := adminReg.Get(targetName)
	if !ok {
		return ""
	}
	return registration.Options.Label
}

// relatedLabel resolves the display label for a foreign key value: the
// related row's configured Label field, or the raw value itself if the
// target model is unregistered, has no Label configured, or the referenced
// row no longer exists — never a hard error (Milestone 14, Q17).
func relatedLabel(ctx context.Context, store *db.Store, models *model.Registry, adminReg *adminregistry.Registry, targetName string, value any) string {
	fallback := fmt.Sprint(value)

	labelField := labelFieldFor(adminReg, targetName)
	if labelField == "" {
		return fallback
	}

	relatedMeta, ok := models.Get(targetName)
	if !ok {
		return fallback
	}

	instancePtr := reflect.New(relatedMeta.Type)
	if err := store.Get(ctx, relatedMeta, value, instancePtr.Interface()); err != nil {
		return fallback
	}

	return formatFieldValue(instancePtr.Elem().FieldByName(labelField))
}

// relatedSelectOptions lists every row of the related model targetName as
// select options, labeled via relatedLabel, with currentValue's option
// marked Selected. Returns (nil, false) if targetName isn't a registered
// model at all — callers fall back to an ordinary numeric input in that
// case, which should only happen if ValidateForeignKeys was skipped.
func relatedSelectOptions(ctx context.Context, store *db.Store, models *model.Registry, adminReg *adminregistry.Registry, targetName string, currentValue string) ([]SelectOption, bool) {
	relatedMeta, ok := models.Get(targetName)
	if !ok {
		return nil, false
	}
	relatedPKField, err := primaryKeyField(relatedMeta)
	if err != nil {
		return nil, false
	}

	sliceType := reflect.SliceOf(relatedMeta.Type)
	destPtr := reflect.New(sliceType)
	if err := store.List(ctx, relatedMeta, db.Query{OrderBy: []string{relatedPKField.Name}}, destPtr.Interface()); err != nil {
		return nil, true
	}

	labelField := labelFieldFor(adminReg, targetName)

	sliceValue := destPtr.Elem()
	options := make([]SelectOption, sliceValue.Len())
	for i := 0; i < sliceValue.Len(); i++ {
		elem := sliceValue.Index(i)
		pkValue := elem.FieldByName(relatedPKField.Name).Interface()
		value := fmt.Sprint(pkValue)

		text := value
		if labelField != "" {
			text = formatFieldValue(elem.FieldByName(labelField))
		}

		options[i] = SelectOption{Value: value, Text: text, Selected: value == currentValue}
	}

	return options, true
}

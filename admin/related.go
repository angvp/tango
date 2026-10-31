package admin

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/i18n"
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
// row no longer exists — never a hard error.
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
	noun := targetNoun(adminReg, targetName, relatedMeta)

	sliceType := reflect.SliceOf(relatedMeta.Type)
	destPtr := reflect.New(sliceType)
	if err := store.List(ctx, relatedMeta, db.Query{OrderBy: []string{relatedPKField.Name}}, destPtr.Interface()); err != nil {
		// The form still renders, with only the empty choice, so an unrelated
		// outage on the related table does not hide the whole page.
		slog.ErrorContext(ctx, "admin: listing related rows for a foreign key select failed", "model", targetName, "error", err)
		return []SelectOption{noneOption(ctx, currentValue, noun)}, true
	}

	labelField := labelFieldFor(adminReg, targetName)

	sliceValue := destPtr.Elem()
	// A foreign key at its zero value is unset (stored as NULL), so every
	// foreign key select offers an explicit way to leave it that way.
	options := make([]SelectOption, 1+sliceValue.Len())
	options[0] = noneOption(ctx, currentValue, noun)
	for i := 0; i < sliceValue.Len(); i++ {
		elem := sliceValue.Index(i)
		pkValue := elem.FieldByName(relatedPKField.Name).Interface()
		value := fmt.Sprint(pkValue)

		options[i+1] = SelectOption{Value: value, Text: relatedChoiceText(elem, labelField, value), Selected: value == currentValue}
	}

	return options, true
}

// targetNoun is the display noun of the related model, which names the
// empty choice of a foreign key select and the add button beside it.
func targetNoun(adminReg *adminregistry.Registry, targetName string, meta model.ModelMeta) string {
	var opts adminregistry.Options
	if registration, ok := adminReg.Get(targetName); ok {
		opts = registration.Options
	}
	return displayNoun(meta, opts)
}

// noneOption is the blank choice that leaves a foreign key unset; it is
// selected while the current value is empty or zero.
func noneOption(ctx context.Context, currentValue, noun string) SelectOption {
	return SelectOption{
		Value:    "",
		Text:     i18n.T(ctx, "admin.fk.none_selected", "No %s selected", noun),
		Selected: currentValue == "" || currentValue == "0",
	}
}

// relatedChoiceText is the text shown for one related row: its labelField
// when one is configured, otherwise its primary key value.
func relatedChoiceText(row reflect.Value, labelField, value string) string {
	if labelField == "" {
		return value
	}
	return formatFieldValue(row.FieldByName(labelField))
}

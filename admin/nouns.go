package admin

import (
	"strings"
	"unicode"

	"github.com/angvp/tango/internal/adminregistry"
	"github.com/angvp/tango/model"
)

// Messages name a model's rows with the display nouns of admin.Options. An
// unset noun falls back to the humanized Go type name, so the raw Go name
// never reaches the page.

// fieldLabel is the label the form shows for a field: the host's Labels
// entry when it has one, else the humanized Go name.
func fieldLabel(opts adminregistry.Options, name string) string {
	if label, ok := opts.Labels[name]; ok {
		return label
	}
	return humanizeFieldName(name)
}

// displayNoun is the noun for one row of the model, as a sentence uses it.
func displayNoun(meta model.ModelMeta, opts adminregistry.Options) string {
	if opts.SingularName != "" {
		return strings.ToLower(opts.SingularName)
	}
	return strings.ToLower(humanizeFieldName(meta.Name))
}

// pluralNoun is the noun for several rows of the model, as a sentence uses it. Unset, it
// is the singular with the usual English plural ending.
func pluralNoun(meta model.ModelMeta, opts adminregistry.Options) string {
	if opts.PluralName != "" {
		return strings.ToLower(opts.PluralName)
	}
	return englishPlural(displayNoun(meta, opts))
}

// sectionName is the capitalized plural noun a page or section is titled with.
func sectionName(meta model.ModelMeta, opts adminregistry.Options) string {
	return capitalizeFirst(pluralNoun(meta, opts))
}

// capitalizeFirst returns s with its first letter in capitals, for a heading.
func capitalizeFirst(s string) string {
	runes := []rune(s)
	if len(runes) == 0 {
		return s
	}
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

// englishPlural applies the common regular endings to a noun.
func englishPlural(noun string) string {
	switch {
	case strings.HasSuffix(noun, "s"), strings.HasSuffix(noun, "x"), strings.HasSuffix(noun, "z"),
		strings.HasSuffix(noun, "ch"), strings.HasSuffix(noun, "sh"):
		return noun + "es"
	case strings.HasSuffix(noun, "y") && len(noun) > 1 && !strings.ContainsRune("aeiou", rune(noun[len(noun)-2])):
		return noun[:len(noun)-1] + "ies"
	default:
		return noun + "s"
	}
}

package openapi_test

import (
	"testing"

	"github.com/angvp/tango"
)

// relOAPost and relOAComment refer to each other through relation fields, so
// describing either one reaches the other and must not recurse forever.
type relOAPost struct {
	ID        int64          `tango:"pk"`
	AccountID int64          `tango:"fk=relOAAccount"`
	Account   *relOAAccount  `tango:"belongs_to=AccountID"`
	Comments  []relOAComment `tango:"has_many=PostID"`
}

type relOAComment struct {
	ID     int64      `tango:"pk"`
	PostID int64      `tango:"fk=relOAPost"`
	Post   *relOAPost `tango:"belongs_to=PostID"`
}

type relOAAccount struct {
	ID   int64  `tango:"pk"`
	Name string `tango:"varchar=100"`
}

func TestMutuallyReferencingRelationFieldsTerminateAndDescribeAsNullableObjectOrArray(t *testing.T) {
	doc := documentOf(t, tango.Path("GET", "/{id}", view, tango.Doc(tango.RouteDoc{
		Responses: map[int]any{200: relOAPost{}},
	})))
	schemas := at(t, doc, "components", "schemas").(map[string]any)

	equal(t, schemas["relOAPost"].(map[string]any)["properties"].(map[string]any)["Comments"], map[string]any{
		"type":  "array",
		"items": map[string]any{"$ref": "#/components/schemas/relOAComment"},
	})
	equal(t, schemas["relOAPost"].(map[string]any)["properties"].(map[string]any)["Account"], map[string]any{
		"oneOf": []any{
			map[string]any{"$ref": "#/components/schemas/relOAAccount"},
			map[string]any{"type": "null"},
		},
	})
	equal(t, schemas["relOAComment"].(map[string]any)["properties"].(map[string]any)["Post"], map[string]any{
		"oneOf": []any{
			map[string]any{"$ref": "#/components/schemas/relOAPost"},
			map[string]any{"type": "null"},
		},
	})
}

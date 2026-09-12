package admin

import "testing"

func TestHumanizeFieldName(t *testing.T) {
	cases := map[string]string{
		"CreatedAt": "Created At",
		"Title":     "Title",
		"UserID":    "User ID",
		"ID":        "ID",
		"HTMLTitle": "HTML Title",
		"A":         "A",
	}
	for input, want := range cases {
		if got := humanizeFieldName(input); got != want {
			t.Errorf("humanizeFieldName(%q) = %q, want %q", input, got, want)
		}
	}
}

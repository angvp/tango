package db

import "testing"

func TestDBColumnNameDerivesSnakeCase(t *testing.T) {
	cases := map[string]string{
		"User":      "user",
		"CreatedAt": "created_at",
		"ID":        "id",
		"UserID":    "user_id",
	}

	for input, want := range cases {
		if got := ColumnName(input); got != want {
			t.Errorf("ColumnName(%q) = %q, want %q", input, got, want)
		}
	}
}

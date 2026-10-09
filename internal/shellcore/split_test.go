package shellcore

import "testing"

func TestBalancedContinuesOnlyWhileBracketsAreOpen(t *testing.T) {
	tests := []struct {
		src  string
		want bool
	}{
		{`1 + 1`, true},
		{`f(1, 2)`, true},
		{`func f() {`, false},
		{"func f() {\n\treturn\n}", true},
		{`x := []int{1, 2,`, false},
		{`m := map[string]int{"a": 1}`, true},
		{`s := "(" + ")"`, true},
		{`s := "{"`, true},
		{`r := '('`, true},
		{"s := `{\n", false},
		{"s := `{`", true},
		{`x := 1 // (`, true},
		{`/* ( */ 1`, true},
		{`/* (`, false},
		{`f(`, false},
		{`)`, true},
	}
	for _, tt := range tests {
		if got := Balanced(tt.src); got != tt.want {
			t.Errorf("Balanced(%q) = %v, want %v", tt.src, got, tt.want)
		}
	}
}

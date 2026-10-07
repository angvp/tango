package sqlident

import "testing"

func TestQuoteEscapesTheStylesQuoteCharacter(t *testing.T) {
	cases := []struct {
		style Style
		name  string
		want  string
	}{
		{Backtick, "user", "`user`"},
		{Backtick, "we`ird", "`we``ird`"},
		{Backtick, `say"hi`, "`say\"hi`"},
		{DoubleQuote, "user", `"user"`},
		{DoubleQuote, `say"hi`, `"say""hi"`},
		{DoubleQuote, "we`ird", "\"we`ird\""},
	}
	for _, tc := range cases {
		if got := Quote(tc.style, tc.name); got != tc.want {
			t.Errorf("Quote(%v, %q) = %s, want %s", tc.style, tc.name, got, tc.want)
		}
	}
}

func TestQuoteAllJoinsQuotedNames(t *testing.T) {
	if got := QuoteAll(DoubleQuote, []string{"id", "order"}); got != `"id", "order"` {
		t.Fatalf("QuoteAll = %s", got)
	}
}

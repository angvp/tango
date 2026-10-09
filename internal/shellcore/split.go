package shellcore

// Balanced reports whether src has no open bracket, string or comment, so
// that a line of input can be evaluated rather than continued on the next.
// Brackets inside strings, runes and comments do not count.
func Balanced(src string) bool {
	depth := 0
	rs := []rune(src)
	for i := 0; i < len(rs); i++ {
		switch c := rs[i]; c {
		case '(', '{', '[':
			depth++
		case ')', '}', ']':
			depth--
		case '"', '\'':
			end, ok := closeQuoted(rs, i, c)
			if !ok {
				return true // an unterminated interpreted string is the interpreter's error to report
			}
			i = end
		case '`':
			end, ok := closeQuoted(rs, i, c)
			if !ok {
				return false // a raw string runs over lines
			}
			i = end
		case '/':
			if i+1 < len(rs) && rs[i+1] == '/' {
				for i < len(rs) && rs[i] != '\n' {
					i++
				}
			} else if i+1 < len(rs) && rs[i+1] == '*' {
				end := -1
				for j := i + 2; j+1 < len(rs); j++ {
					if rs[j] == '*' && rs[j+1] == '/' {
						end = j + 1
						break
					}
				}
				if end < 0 {
					return false
				}
				i = end
			}
		}
	}
	return depth <= 0
}

// closeQuoted finds the quote that closes the one opened at rs[start].
func closeQuoted(rs []rune, start int, quote rune) (int, bool) {
	for i := start + 1; i < len(rs); i++ {
		switch {
		case rs[i] == '\\' && quote != '`':
			i++
		case rs[i] == quote:
			return i, true
		case rs[i] == '\n' && quote != '`':
			return 0, false
		}
	}
	return 0, false
}

package openapi

import (
	"fmt"
	"strings"
)

// pathParam is one placeholder of a route pattern.
type pathParam struct {
	name     string
	wildcard bool
}

// convertPattern turns a chi pattern into an OpenAPI path template and the
// parameters it declares. "{id:[0-9]+}" becomes "{id}"; a trailing "*" becomes
// "{path}" (or "{path2}" and so on when "path" is already a placeholder).
func convertPattern(pattern string) (string, []pathParam, error) {
	var out strings.Builder
	var params []pathParam
	seen := map[string]bool{}
	for i := 0; i < len(pattern); i++ {
		switch c := pattern[i]; c {
		case '{':
			end := matchingBrace(pattern, i)
			if end < 0 {
				return "", nil, fmt.Errorf("unbalanced placeholder in %q", pattern)
			}
			name, _, _ := strings.Cut(pattern[i+1:end], ":")
			name = strings.TrimSpace(name)
			if name == "" {
				return "", nil, fmt.Errorf("empty placeholder in %q", pattern)
			}
			out.WriteString("{" + name + "}")
			if !seen[name] {
				seen[name] = true
				params = append(params, pathParam{name: name})
			}
			i = end
		case '*':
			name := "path"
			for n := 2; seen[name]; n++ {
				name = fmt.Sprintf("path%d", n)
			}
			seen[name] = true
			params = append(params, pathParam{name: name, wildcard: true})
			out.WriteString("{" + name + "}")
		default:
			out.WriteByte(c)
		}
	}
	return out.String(), params, nil
}

// matchingBrace returns the index of the "}" closing the "{" at open, allowing
// nested braces in a regular expression such as {id:[0-9]{3}}.
func matchingBrace(s string, open int) int {
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

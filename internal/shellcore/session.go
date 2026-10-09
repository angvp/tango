package shellcore

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"reflect"
	"strings"

	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
)

// PanicError is a panic recovered from the interpreter. The session that
// raised it is still usable.
type PanicError struct{ Value any }

func (e *PanicError) Error() string { return fmt.Sprint(e.Value) }

// Session is one interpreter whose variables and imports persist across Eval
// calls.
type Session struct {
	interp *interp.Interpreter
	// limit is the most runes of one printed line; 0 never truncates.
	limit int
	// results is how many values each exported function returns, keyed by
	// the name interpreted code calls it by.
	results map[string]int
	// seq numbers the temporary variables that hold a call's results.
	seq int
}

// NewSession starts an interpreter that writes what interpreted code prints
// to out and errOut, and reads from in.
func NewSession(in io.Reader, out, errOut io.Writer, limit int) (*Session, error) {
	i := interp.New(interp.Options{Stdin: in, Stdout: out, Stderr: errOut})
	if err := i.Use(stdlib.Symbols); err != nil {
		return nil, fmt.Errorf("load the standard library symbols: %w", err)
	}
	return &Session{interp: i, limit: limit, results: map[string]int{}}, nil
}

// Export makes funcs callable from interpreted code: by bare name when
// qualifier is empty, otherwise as qualifier.Name. A call to one of them as
// the last expression of a line shows every value it returns, including a
// non-nil error, which the interpreter would otherwise drop.
func (s *Session) Export(qualifier string, funcs map[string]any) error {
	exports := interp.Exports{}
	path := "tangoshell"
	if qualifier != "" {
		path = qualifier
	}
	symbols := map[string]reflect.Value{}
	for name, fn := range funcs {
		rv := reflect.ValueOf(fn)
		symbols[name] = rv
		key := name
		if qualifier != "" {
			key = qualifier + "." + name
		}
		if rv.Kind() == reflect.Func {
			s.results[key] = rv.Type().NumOut()
		}
	}
	exports[path+"/"+path] = symbols
	if err := s.interp.Use(exports); err != nil {
		return fmt.Errorf("export %s: %w", path, err)
	}
	importLine := fmt.Sprintf("import . %q", path)
	if qualifier != "" {
		importLine = fmt.Sprintf("import %q", path)
	}
	if _, err := s.interp.Eval(importLine); err != nil {
		return fmt.Errorf("import %s: %w", path, err)
	}
	return nil
}

// Eval runs code and returns what to print for it: the formatted value of a
// final expression, and nothing for declarations, assignments and nil. A
// non-nil error value, whether returned by a call or the value of the
// expression, comes back as the error. A panic in the interpreter comes back
// as a *PanicError.
func (s *Session) Eval(code string) (printed string, err error) {
	defer func() {
		if r := recover(); r != nil {
			printed, err = "", &PanicError{Value: r}
		}
	}()
	final, ok := splitFinalExpr(code)
	if !ok {
		_, err := s.interp.Eval(code)
		return "", err
	}
	if final.prefix != "" {
		if _, err := s.interp.Eval(final.prefix); err != nil {
			return "", err
		}
	}
	values, err := s.evalExpr(final)
	if err != nil {
		return "", err
	}
	return s.render(values, quietCalls[final.callee])
}

// evalExpr evaluates the final expression of a line and returns its values.
func (s *Session) evalExpr(final finalExpr) ([]reflect.Value, error) {
	n, known := s.resultCount(final.callee)
	if known && n == 0 {
		// The interpreter would hand back the previous line's value.
		_, err := s.interp.Eval(final.expr)
		return nil, err
	}
	if !known || n < 2 {
		v, err := s.interp.Eval(final.expr)
		if err != nil {
			return nil, err
		}
		return []reflect.Value{v}, nil
	}
	// The interpreter returns only the first value of a call, so hold all
	// n values in variables and read them back.
	s.seq++
	names := make([]string, n)
	for i := range names {
		names[i] = fmt.Sprintf("__tango_%d_%d", s.seq, i)
	}
	if _, err := s.interp.Eval(strings.Join(names, ", ") + " := " + final.expr); err != nil {
		return nil, err
	}
	values := make([]reflect.Value, n)
	for i, name := range names {
		v, err := s.interp.Eval(name)
		if err != nil {
			return nil, err
		}
		values[i] = v
	}
	return values, nil
}

// resultCount is how many values the function named callee returns, when
// the shell knows (known): its own exports and the standard library's functions.
func (s *Session) resultCount(callee string) (n int, known bool) {
	if n, ok := s.results[callee]; ok {
		return n, true
	}
	pkg, name, qualified := strings.Cut(callee, ".")
	if !qualified {
		return 0, false
	}
	if rv, ok := stdlibSymbol(pkg, name); ok && rv.Kind() == reflect.Func {
		return rv.Type().NumOut(), true
	}
	return 0, false
}

// stdlibIndex maps a standard library package's name to its symbols. Two
// packages with one name (math/rand, crypto/rand) are left out: which one an
// import means is not known here.
var stdlibIndex = func() map[string]map[string]reflect.Value {
	index := map[string]map[string]reflect.Value{}
	ambiguous := map[string]bool{}
	for path, symbols := range stdlib.Symbols {
		name := path[strings.LastIndex(path, "/")+1:]
		if _, taken := index[name]; taken {
			ambiguous[name] = true
		}
		index[name] = symbols
	}
	for name := range ambiguous {
		delete(index, name)
	}
	return index
}()

func stdlibSymbol(pkg, name string) (reflect.Value, bool) {
	symbols, ok := stdlibIndex[pkg]
	if !ok {
		return reflect.Value{}, false
	}
	rv, ok := symbols[name]
	return rv, ok
}

// quietCalls print their own output and return only a byte count and an
// error; the count is noise in a shell.
var quietCalls = map[string]bool{
	"fmt.Print": true, "fmt.Println": true, "fmt.Printf": true,
	"fmt.Fprint": true, "fmt.Fprintln": true, "fmt.Fprintf": true,
}

// render turns the values of the final expression into a printed result,
// or the error among them.
func (s *Session) render(values []reflect.Value, quiet bool) (string, error) {
	var lines []string
	for i, v := range values {
		if quiet && i == 0 {
			continue
		}
		if !v.IsValid() || !v.CanInterface() {
			continue
		}
		value := valueOf(v)
		if value == nil {
			continue
		}
		if err, isErr := value.(error); isErr {
			return "", err
		}
		if text := Format(value, s.limit); text != "" {
			lines = append(lines, text)
		}
	}
	return strings.Join(lines, "\n"), nil
}

func valueOf(v reflect.Value) any {
	switch v.Kind() {
	case reflect.Interface, reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		if v.IsNil() {
			return nil
		}
	}
	return v.Interface()
}

// finalExpr is the last statement of a line when it is an expression: the
// value the shell prints.
type finalExpr struct {
	prefix string // everything before the expression
	expr   string
	// callee is the name of the exported function the expression calls, or "".
	callee string
}

const wrapHead = "package p\nfunc _() {\n"

// splitFinalExpr reports whether code ends in an expression statement.
// Declarations, imports and assignments are not expressions and print nothing.
func splitFinalExpr(code string) (finalExpr, bool) {
	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, "", "package p\n"+code, 0); err == nil {
		return finalExpr{}, false // only top-level declarations
	}
	wrapped := wrapHead + code + "\n}"
	file, err := parser.ParseFile(fset, "", wrapped, 0)
	if err != nil {
		return finalExpr{}, false
	}
	body := file.Decls[0].(*ast.FuncDecl).Body.List
	if len(body) == 0 {
		return finalExpr{}, false
	}
	last, isExpr := body[len(body)-1].(*ast.ExprStmt)
	if !isExpr {
		return finalExpr{}, false
	}
	start := fset.Position(last.Pos()).Offset - len(wrapHead)
	end := fset.Position(last.End()).Offset - len(wrapHead)
	return finalExpr{prefix: code[:start], expr: code[start:end], callee: calleeName(last.X)}, true
}

// calleeName is the name a call expression calls its function by, as Export
// keys them ("List", "project.Reindex"), or "" for anything else.
func calleeName(e ast.Expr) string {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return ""
	}
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name
	case *ast.SelectorExpr:
		if pkg, ok := fn.X.(*ast.Ident); ok {
			return pkg.Name + "." + fn.Sel.Name
		}
	}
	return ""
}

package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// openAPICommand prints the project's OpenAPI document, or writes it to the
// file named by -o. The app produces the document (go run . -tango-openapi);
// the CLI only moves it, so a failed run leaves an existing file untouched.
func openAPICommand(ctx context.Context, runner Runner, dir string, args []string, stdout io.Writer, stderr io.Writer) int {
	output := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-o", "--output":
			if i+1 >= len(args) {
				fmt.Fprintf(stderr, "tango openapi: %s needs a file name\n", args[i])
				return 2
			}
			output = args[i+1]
			i++
		default:
			fmt.Fprintf(stderr, "tango openapi: unknown argument %q\n", args[i])
			return 2
		}
	}
	var document bytes.Buffer
	if code := runGo(ctx, runner, dir, &document, stderr, []string{"run", ".", "-tango-openapi"}); code != 0 {
		return code
	}
	if output == "" {
		if _, err := stdout.Write(document.Bytes()); err != nil {
			fmt.Fprintf(stderr, "tango openapi: %v\n", err)
			return 1
		}
		return 0
	}
	target := output
	if !filepath.IsAbs(target) {
		target = filepath.Join(dir, target)
	}
	if err := os.WriteFile(target, document.Bytes(), 0o644); err != nil {
		fmt.Fprintf(stderr, "tango openapi: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "wrote %s\n", output)
	return 0
}

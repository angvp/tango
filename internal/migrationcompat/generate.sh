#!/usr/bin/env bash
# Regenerates one version's migration-file fixtures for the Generated-file
# contract tests. See README.md in this directory.
#
#   ./generate.sh v0.0.2          # the released v0.0.2 framework and CLI
#   ./generate.sh v0.1.0 local    # this checkout, saved as v0.1.0 (before tagging it)
#   ./generate.sh unreleased local
#
# It builds a small app against that tanGO version, evolves its models
# through a fixed series of stages, and runs that version's own
# `tango makemigrations` after each one. The generated migrations package
# and the final -tango-dump-models output are copied here, under the
# version's directory, replacing whatever was there.
set -euo pipefail

if [[ $# -lt 1 || $# -gt 2 || ( $# -eq 2 && $2 != local ) ]]; then
	echo "usage: $0 <version> [local]" >&2
	exit 2
fi
version=$1
here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../.." && pwd)
out="$here/$(echo "$version" | tr . _)"
work=$(mktemp -d)
trap 'command rm -rf "$work"' EXIT

app="$work/app"
mkdir -p "$app"
cd "$app"
cat >go.mod <<EOF
module compatapp

go 1.27
EOF
if [[ ${2:-} == local ]]; then
	go mod edit -require=github.com/angvp/tango@v0.0.0 -replace=github.com/angvp/tango="$repo"
	(cd "$repo" && go build -o "$work/tango" ./cmd/tango)
	tango=("$work/tango")
else
	go mod edit -require=github.com/angvp/tango@"$version"
	tango=(go run "github.com/angvp/tango/cmd/tango@$version")
fi

# Releases before v0.1.0 have no --rename or --allow-drop, so they can
# neither rename nor be told a drop is intended.
case $version in
v0.0.1 | v0.0.2) extended=false ;;
*) extended=true ;;
esac

cat >main.go <<'EOF'
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/angvp/tango"
)

// main only answers -tango-dump-models, the one flag makemigrations runs.
func main() {
	if len(os.Args) != 2 || os.Args[1] != "-tango-dump-models" {
		fmt.Fprintln(os.Stderr, "usage: compatapp -tango-dump-models")
		os.Exit(2)
	}
	app := tango.NewApp("blog", func(r *tango.Registry) error {
		for _, m := range models() {
			if err := r.Models().Register(m); err != nil {
				return err
			}
		}
		return nil
	})
	dumped, err := tango.DumpModels(tango.Config{InstalledApps: []tango.App{app}})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := json.NewEncoder(os.Stdout).Encode(dumped); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
EOF

# stage writes models.go from its arguments (Go type declarations) and
# runs makemigrations with the stage's name and any extra flags after --.
stage() {
	local name=$1 decls=$2
	shift 2
	[[ ${1:-} == -- ]] && shift
	{
		echo 'package main'
		echo
		echo 'import "time"'
		echo
		echo 'var _ time.Time'
		echo
		echo "$decls"
	} >models.go
	echo "== $version: $name" >&2
	go mod tidy
	"${tango[@]}" makemigrations --name "$name" "$@"
}

drop_flags() {
	if $extended; then
		printf -- '--allow-drop=%s\n' "$@"
	fi
}

stage initial '
type Author struct {
	ID   int64 `tango:"pk"`
	Name string `tango:"unique"`
}

type Post struct {
	ID        int64 `tango:"pk"`
	Title     string
	Body      string
	Score     int64
	AuthorID  int64 `tango:"fk=Author"`
	CreatedAt time.Time
}

type Draft struct {
	ID   int64 `tango:"pk"`
	Note string
}

type Comment struct {
	ID     int64 `tango:"pk"`
	PostID int64 `tango:"fk=Post"`
	Text   string
}

func models() []any { return []any{Author{}, Post{}, Draft{}, Comment{}} }'

stage add_fields '
type Author struct {
	ID   int64 `tango:"pk"`
	Name string `tango:"unique"`
}

type Post struct {
	ID        int64 `tango:"pk"`
	Title     string
	Body      string
	Score     int64
	Published bool
	Rating    float64
	AuthorID  int64 `tango:"fk=Author"`
	CreatedAt time.Time
}

type Draft struct {
	ID   int64 `tango:"pk"`
	Note string
}

type Comment struct {
	ID     int64 `tango:"pk"`
	PostID int64 `tango:"fk=Post"`
	Text   string
}

func models() []any { return []any{Author{}, Post{}, Draft{}, Comment{}} }'

stage constraints '
type Author struct {
	ID   int64 `tango:"pk"`
	Name string
}

type Post struct {
	ID        int64  `tango:"pk"`
	Title     string `tango:"unique"`
	Body      string `tango:"index"`
	Score     int64
	Published bool
	Rating    float64
	AuthorID  int64 `tango:"fk=Author"`
	CreatedAt time.Time
}

type Draft struct {
	ID   int64 `tango:"pk"`
	Note string
}

type Comment struct {
	ID     int64 `tango:"pk"`
	PostID int64 `tango:"fk=Post"`
	Text   string
}

func models() []any { return []any{Author{}, Post{}, Draft{}, Comment{}} }'

stage drop_index '
type Author struct {
	ID   int64 `tango:"pk"`
	Name string
}

type Post struct {
	ID        int64  `tango:"pk"`
	Title     string `tango:"unique"`
	Body      string
	Score     int64
	Published bool
	Rating    float64
	AuthorID  int64 `tango:"fk=Author"`
	CreatedAt time.Time
}

type Draft struct {
	ID   int64 `tango:"pk"`
	Note string
}

type Comment struct {
	ID     int64 `tango:"pk"`
	PostID int64 `tango:"fk=Post"`
	Text   string
}

func models() []any { return []any{Author{}, Post{}, Draft{}, Comment{}} }'

# shellcheck disable=SC2046
stage drop_field '
type Author struct {
	ID   int64 `tango:"pk"`
	Name string
}

type Post struct {
	ID        int64  `tango:"pk"`
	Title     string `tango:"unique"`
	Body      string
	Score     int64
	Rating    float64
	AuthorID  int64 `tango:"fk=Author"`
	CreatedAt time.Time
}

type Draft struct {
	ID   int64 `tango:"pk"`
	Note string
}

type Comment struct {
	ID     int64 `tango:"pk"`
	PostID int64 `tango:"fk=Post"`
	Text   string
}

func models() []any { return []any{Author{}, Post{}, Draft{}, Comment{}} }' -- $(drop_flags blog.Post.Published)

# shellcheck disable=SC2046
stage drop_model '
type Author struct {
	ID   int64 `tango:"pk"`
	Name string
}

type Post struct {
	ID        int64  `tango:"pk"`
	Title     string `tango:"unique"`
	Body      string
	Score     int64
	Rating    float64
	AuthorID  int64 `tango:"fk=Author"`
	CreatedAt time.Time
}

type Comment struct {
	ID     int64 `tango:"pk"`
	PostID int64 `tango:"fk=Post"`
	Text   string
}

func models() []any { return []any{Author{}, Post{}, Comment{}} }' -- $(drop_flags blog.Draft)

if $extended; then
	stage rename_field '
type Author struct {
	ID   int64 `tango:"pk"`
	Name string
}

type Post struct {
	ID        int64  `tango:"pk"`
	Heading   string `tango:"unique"`
	Body      string
	Score     int64
	Rating    float64
	AuthorID  int64 `tango:"fk=Author"`
	CreatedAt time.Time
}

type Comment struct {
	ID     int64 `tango:"pk"`
	PostID int64 `tango:"fk=Post"`
	Text   string
}

func models() []any { return []any{Author{}, Post{}, Comment{}} }' -- --rename blog.Post.Title=Heading

	stage rename_model '
type Writer struct {
	ID   int64 `tango:"pk"`
	Name string
}

type Post struct {
	ID        int64  `tango:"pk"`
	Heading   string `tango:"unique"`
	Body      string
	Score     int64
	Rating    float64
	AuthorID  int64 `tango:"fk=Writer"`
	CreatedAt time.Time
}

type Comment struct {
	ID     int64 `tango:"pk"`
	PostID int64 `tango:"fk=Post"`
	Text   string
}

func models() []any { return []any{Writer{}, Post{}, Comment{}} }' -- --rename blog.Author=Writer

	stage widen_type '
type Writer struct {
	ID   int64 `tango:"pk"`
	Name string
}

type Post struct {
	ID        int64  `tango:"pk"`
	Heading   string `tango:"unique"`
	Body      string
	Score     float64
	Rating    float64
	AuthorID  int64 `tango:"fk=Writer"`
	CreatedAt time.Time
}

type Comment struct {
	ID     int64 `tango:"pk"`
	PostID int64 `tango:"fk=Post"`
	Text   string
}

func models() []any { return []any{Writer{}, Post{}, Comment{}} }'
fi

command rm -rf "$out"
mkdir -p "$out"
cp -R migrations "$out/migrations"
go run . -tango-dump-models >"$out/models.json"
echo "wrote $out" >&2

// Package shell is the interactive console behind `tango shell`: a Go
// interpreter booted with a project's configuration, database and model
// registry, without serving HTTP.
//
// A project's shell/main.go calls [Run]; nothing else here is public.
// Importing this package links the Yaegi interpreter, so a server's main
// package should not import it. The shell executes local developer code
// with the project's credentials: it is not a sandbox and not a remote
// administration surface.
package shell

import (
	"context"
	"fmt"
	"os"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/internal/shellcore"
)

// Options configures a shell session beyond its command line.
type Options struct {
	// ReadOnly makes every built-in helper that writes fail. The --readonly
	// flag has the same effect; either one turns it on.
	ReadOnly bool

	// Helpers are project-defined functions reachable from the session as
	// project.Name(...). Not wired up yet.
	Helpers map[string]any

	// DatabaseLabel is shown at startup so a person can see which database
	// the session works on. It is display-only text that the project
	// derives from its database configuration after removing every part of
	// it that could be a credential (user name, password, URL userinfo). It
	// is never a DSN or any other connection value.
	DatabaseLabel string
}

// Run boots config's registry against store without serving HTTP, then runs
// a shell session for args (os.Args[1:] of a project's shell/main.go) and
// returns the process exit code.
//
// The accepted arguments are -c EXPR (evaluate EXPR and exit), --readonly and
// --help. With no -c, the session reads standard input. Exit codes: 0 for a
// session that ended cleanly or --help, 1 for the first error of a
// non-interactive session or a project that fails to boot, 2 for a command
// line that is not understood, and 130 when Ctrl-C interrupts an evaluation.
func Run(ctx context.Context, config tango.Config, store *db.Store, args []string, opts Options) int {
	return run(ctx, config, store, args, opts, shellcore.IO{In: os.Stdin, Out: os.Stdout, Err: os.Stderr})
}

// run is Run with its streams supplied, so tests can drive it.
func run(ctx context.Context, config tango.Config, store *db.Store, args []string, opts Options, streams shellcore.IO) int {
	registry, err := boot(config, store)
	if err != nil {
		fmt.Fprintf(streams.Err, "tango shell: %v\n", err)
		return shellcore.ExitError
	}
	return shellcore.Run(ctx, streams, shellcore.Boot{
		Registry:      registry,
		Store:         store,
		ReadOnly:      opts.ReadOnly,
		Helpers:       opts.Helpers,
		DatabaseLabel: opts.DatabaseLabel,
	}, args)
}

// boot registers config's apps and wires their models into store, the way
// serving does, but starts nothing: no HTTP server, jobs or lifecycles.
func boot(config tango.Config, store *db.Store) (*tango.Registry, error) {
	if store == nil {
		return nil, fmt.Errorf("no database store was given")
	}
	registry, err := tango.BuildRegistry(config)
	if err != nil {
		return nil, fmt.Errorf("build registry: %w", err)
	}
	if err := registry.RunRegistration(); err != nil {
		return nil, fmt.Errorf("run registration: %w", err)
	}
	registry.SetStore(store)
	return registry, nil
}

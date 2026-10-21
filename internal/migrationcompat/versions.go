// Package migrationcompat holds, under one directory per tanGO version, the
// migration files that version's own `tango makemigrations` wrote, and lists
// them for the Generated-file contract tests. See README.md.
package migrationcompat

import (
	"github.com/angvp/tango/internal/migrationcompat/unreleased/migrations"
	v001 "github.com/angvp/tango/internal/migrationcompat/v0_0_1/migrations"
	v002 "github.com/angvp/tango/internal/migrationcompat/v0_0_2/migrations"
	v010 "github.com/angvp/tango/internal/migrationcompat/v0_1_0/migrations"
	v020 "github.com/angvp/tango/internal/migrationcompat/v0_2_0/migrations"
	v030 "github.com/angvp/tango/internal/migrationcompat/v0_3_0/migrations"
	v031 "github.com/angvp/tango/internal/migrationcompat/v0_3_1/migrations"
	v040 "github.com/angvp/tango/internal/migrationcompat/v0_4_0/migrations"
	v050 "github.com/angvp/tango/internal/migrationcompat/v0_5_0/migrations"
	v060 "github.com/angvp/tango/internal/migrationcompat/v0_6_0/migrations"
	"github.com/angvp/tango/migration"
)

// Generator is one generator's fixtures.
type Generator struct {
	Dir        string                // the fixture directory, beside this file
	Migrations []migration.Migration // what its migration files compile to
}

// Generators is every fixture directory. Add a release's here when
// generate.sh adds it.
var Generators = []Generator{
	{Dir: "v0_0_1", Migrations: v001.Migrations},
	{Dir: "v0_0_2", Migrations: v002.Migrations},
	{Dir: "v0_1_0", Migrations: v010.Migrations},
	{Dir: "v0_2_0", Migrations: v020.Migrations},
	{Dir: "v0_3_0", Migrations: v030.Migrations},
	{Dir: "v0_3_1", Migrations: v031.Migrations},
	{Dir: "v0_4_0", Migrations: v040.Migrations},
	{Dir: "v0_5_0", Migrations: v050.Migrations},
	{Dir: "v0_6_0", Migrations: v060.Migrations},
	{Dir: "unreleased", Migrations: migrations.Migrations},
}

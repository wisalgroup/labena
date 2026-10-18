// Package migrations provides SQL templates for opt-in application migrations.
// Templates are published by console commands; drivers never execute this SQL.
package migrations

import "embed"

// Templates contains unversioned PostgreSQL and MySQL table definitions.
//
//go:embed postgres/*.sql mysql/*.sql
var Templates embed.FS

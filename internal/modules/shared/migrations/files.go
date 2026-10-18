// Package migrations embeds application-wide SQL migrations.
// Module-owned SQL belongs in each module's migrations package.
package migrations

import "embed"

// Files is registered by app assembly. The wildcard includes documentation so
// the application builds before any SQL is added. Only SQL files are collected.
// Rebuild the application after adding or publishing migrations.
//
//go:embed *
var Files embed.FS

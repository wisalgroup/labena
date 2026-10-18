// Package migrations embeds the SQL owned by media.
package migrations

import "embed"

// Files is registered before startup; migrations only run through the console.
//
//go:embed *.sql
var Files embed.FS

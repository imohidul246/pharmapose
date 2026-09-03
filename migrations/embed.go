// Package migrations embeds the SQL migration files so the binary is fully self-contained.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS

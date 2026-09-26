package migrations

import "embed"

// FS holds SQL migration files applied at startup.
//
//go:embed *.sql
var FS embed.FS

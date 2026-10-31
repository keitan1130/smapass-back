package migrations

import "embed"

// Files embeds the reviewed schema into the migration binary.
//
//go:embed *.sql
var Files embed.FS

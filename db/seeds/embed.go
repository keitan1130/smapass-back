//go:build integration

package seeds

import _ "embed"

// Development is only embedded in integration tests, never in the API binary.
//
//go:embed development.sql
var Development string

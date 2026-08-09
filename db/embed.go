// Package db embeds the SQL migration and seed corpus so a single static binary
// can migrate a database with no filesystem dependencies.
package db

import "embed"

// Migrations holds every versioned migration file.
//
//go:embed migrations/*.sql
var Migrations embed.FS

// Seeds holds optional reference data loaded by `migrate seed`.
//
//go:embed seeds/*.sql
var Seeds embed.FS

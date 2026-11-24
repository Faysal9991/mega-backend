package migrations

import (
	"embed"
	"io/fs"
)

// Embedded files are immutable deployment artifacts, never business fixtures.
//
//go:embed *.sql
var files embed.FS

func Source() fs.FS { return files }

//go:build !cgo
// +build !cgo

package storage

import _ "modernc.org/sqlite"

const sqliteDriverName = "sqlite"

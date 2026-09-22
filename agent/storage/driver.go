package storage

import (
	"net/url"
	"path/filepath"
	"strings"
)

func sqliteDataSource(absolutePath string) string {
	normalizedPath := filepath.ToSlash(absolutePath)
	// A leading slash keeps a Windows drive letter in the URL path rather than
	// making it look like a URI host. SQLite accepts file:///C:/... locally.
	if filepath.VolumeName(absolutePath) != "" && !strings.HasPrefix(normalizedPath, "/") {
		normalizedPath = "/" + normalizedPath
	}
	dsnURL := url.URL{Scheme: "file", Path: normalizedPath}
	query := dsnURL.Query()
	query.Set("_busy_timeout", "5000")
	query.Set("_foreign_keys", "on")
	query.Set("_journal_mode", "WAL")
	query.Set("_synchronous", "FULL")
	dsnURL.RawQuery = query.Encode()
	return dsnURL.String()
}

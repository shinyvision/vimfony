package utils

import (
	"net/url"
	"slices"
	"strings"
)

// Converts a "file://" URI to a filesystem path.
func UriToPath(u string) string {
	if strings.HasPrefix(u, "file://") {
		uu, err := url.Parse(u)
		if err == nil {
			return uu.Path
		}
	}
	return u
}

// Converts a filesystem path to a "file://" URI.
func PathToURI(p string) string {
	u := url.URL{Scheme: "file", Path: p}
	return u.String()
}

// Appends a string to a slice only if it's not already present.
func AppendUnique(slice []string, v string) []string {
	if slices.Contains(slice, v) {
		return slice
	}
	return append(slice, v)
}

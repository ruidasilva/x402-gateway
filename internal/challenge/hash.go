package challenge

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// HashBody returns the SHA-256 hex digest of the request body.
func HashBody(body []byte) string {
	h := sha256.Sum256(body)
	return hex.EncodeToString(h[:])
}

// HashQuery returns the SHA-256 hex digest of sorted query parameters.
// Keys are sorted lexicographically; each key=value pair is joined with "&".
func HashQuery(query url.Values) string {
	keys := make([]string, 0, len(query))
	for k := range query {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var parts []string
	for _, k := range keys {
		vals := query[k]
		sort.Strings(vals)
		for _, v := range vals {
			parts = append(parts, k+"="+v)
		}
	}

	canonical := strings.Join(parts, "&")
	h := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(h[:])
}

// HashHeaders returns the SHA-256 hex digest of selected headers in canonical form.
// Only the specified header keys are included, lowercased and sorted.
func HashHeaders(headers http.Header, keys []string) string {
	sortedKeys := make([]string, len(keys))
	copy(sortedKeys, keys)
	sort.Strings(sortedKeys)

	var parts []string
	for _, k := range sortedKeys {
		lk := strings.ToLower(k)
		val := headers.Get(k)
		parts = append(parts, lk+":"+strings.TrimSpace(val))
	}

	canonical := strings.Join(parts, "\n")
	h := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(h[:])
}

// Copyright 2026 Merkle Works
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//


package challenge

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"sort"
	"strings"
)

// HashBody returns the SHA-256 hex digest of the request body.
// Per spec §4: SHA256(raw_body_bytes) → hex. If no body: SHA256("") → hex.
func HashBody(body []byte) string {
	h := sha256.Sum256(body)
	return hex.EncodeToString(h[:])
}

// HashHeaders returns the SHA-256 hex digest of selected headers in canonical form.
// Per spec §4 (Canonical header-binding string):
//  1. Lowercase each selected header name.
//  2. Trim optional whitespace around the header value.
//  3. Sort headers by header name in ascending byte order.
//  4. Concatenate each as name:value\n (a single LF).
//  5. SHA256(utf8(canonical_string)) → hex.
func HashHeaders(headers http.Header, keys []string) string {
	sortedKeys := make([]string, len(keys))
	copy(sortedKeys, keys)
	sort.Strings(sortedKeys)

	var parts []string
	for _, k := range sortedKeys {
		lk := strings.ToLower(k)
		val := strings.TrimSpace(headers.Get(k))
		parts = append(parts, lk+":"+val)
	}

	// Each header line ends with \n per spec
	canonical := strings.Join(parts, "\n")
	if len(parts) > 0 {
		canonical += "\n"
	}
	h := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(h[:])
}

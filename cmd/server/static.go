// Copyright 2026 Merkle Works
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//


package main

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed static/*
var staticFS embed.FS

// handleDashboardSPA serves the React SPA.
// Static files are served directly. All other paths fall back to index.html
// to support client-side routing.
func handleDashboardSPA() http.HandlerFunc {
	// Strip the "static/" prefix from the embedded filesystem
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic("failed to create sub-filesystem for static: " + err.Error())
	}
	fileServer := http.FileServer(http.FS(sub))

	return func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Skip API and non-SPA routes
		if strings.HasPrefix(path, "/api/") ||
			strings.HasPrefix(path, "/health") ||
			strings.HasPrefix(path, "/nonce/") ||
			strings.HasPrefix(path, "/delegate/") ||
			strings.HasPrefix(path, "/v1/") ||
			strings.HasPrefix(path, "/demo/") ||
			strings.HasPrefix(path, "/playground/") {
			http.NotFound(w, r)
			return
		}

		// Try to serve the exact file from the static directory
		cleanPath := strings.TrimPrefix(path, "/")
		if cleanPath == "" {
			cleanPath = "index.html"
		}

		if _, err := fs.Stat(sub, cleanPath); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}

		// Fallback to index.html for SPA client-side routing
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	}
}

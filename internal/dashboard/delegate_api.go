// Copyright 2026 Merkle Works
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0


package dashboard

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
)

// handleDelegateProxy proxies delegation requests to the delegator service.
// This allows the dashboard testing flow to call the delegator without the
// browser needing direct access or dealing with CORS / mixed-content issues.
//
// When DELEGATOR_EMBEDDED=true, proxies to the gateway's own /delegate/x402
// route (same-origin). When false, proxies to the external delegator URL.
func (d *DashboardAPI) handleDelegateProxy() http.HandlerFunc {
	logger := slog.Default().With("component", "dashboard.delegate-proxy")

	return func(w http.ResponseWriter, r *http.Request) {
		// Read the incoming request body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": "failed to read request body: " + err.Error(),
			})
			return
		}

		// Build the target delegator URL.
		// DelegatorInternalURL is the server-side URL (e.g. http://x402-delegator:8403
		// in Docker). Falls back to DelegatorURL, then localhost.
		var targetURL string
		if d.cfg.DelegatorEmbedded {
			// Embedded: the gateway itself hosts /delegate/x402
			targetURL = fmt.Sprintf("http://localhost:%d/delegate/x402", d.cfg.Port)
		} else {
			delegatorURL := d.cfg.DelegatorInternalURL
			if delegatorURL == "" {
				delegatorURL = d.cfg.DelegatorURL
			}
			if delegatorURL == "" {
				delegatorURL = fmt.Sprintf("http://localhost:%d", d.cfg.DelegatorPort)
			}
			targetURL = delegatorURL + "/delegate/x402"
		}

		logger.Info("proxying delegation request", "target", targetURL)

		proxyReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, targetURL, bytes.NewReader(body))
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"error": "failed to create proxy request: " + err.Error(),
			})
			return
		}
		proxyReq.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(proxyReq)
		if err != nil {
			logger.Warn("delegation proxy failed", "error", err)
			writeJSON(w, http.StatusBadGateway, map[string]any{
				"error": "delegator unreachable: " + err.Error(),
			})
			return
		}
		defer resp.Body.Close()

		// Forward the response as-is
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{
				"error": "failed to read delegator response: " + err.Error(),
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(respBody)
	}
}


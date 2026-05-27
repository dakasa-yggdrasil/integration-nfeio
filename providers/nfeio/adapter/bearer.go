package adapter

import "net/http"

// injectAuth sets the NFe.io Authorization header. NFe.io uses the raw
// API key as the header value with NO "Bearer " prefix — confirmed
// against dakasa-enterprise-payments-api's legacy client (client/nfe-io.go)
// that this adapter replaces.
func injectAuth(req *http.Request, apiKey string) {
	if apiKey != "" {
		req.Header.Set("Authorization", apiKey)
	}
	req.Header.Set("Accept", "application/json")
}

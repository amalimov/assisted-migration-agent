// Package server hosts the local HTTP API and optional bundled UI.
//
// NewServer registers API groups with request logging, panic recovery, and
// OpenAPI validation when a group provides a Swagger document. In "prod" mode
// it serves static UI files with an SPA fallback; "dev" mode serves only the API.
// Both modes use HTTP. The server listens on the configured address and port,
// defaulting to 127.0.0.1 when the listen address is empty. Stop performs a
// graceful shutdown.
package server

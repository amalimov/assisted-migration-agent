// Package server provides the HTTP server for the assisted-migration-agent.
//
// The server uses the Gin web framework and supports two modes of operation:
// development (HTTP) and production (HTTPS with auto-generated TLS certificates).
//
// # Architecture Overview
//
//	┌───────────────────────────────────────────────────────────────┐
//	│                         HTTP Server                           │
//	├───────────────────────────────────────────────────────────────┤
//	│                                                               │
//	│  Production Mode (TLS)          Development Mode              │
//	│  ┌─────────────────────┐        ┌─────────────────────┐       │
//	│  │ HTTPS :8000         │        │ HTTP :8000          │       │
//	│  │ Self-signed cert    │        │ No TLS              │       │
//	│  │ Static file serving │        │ API only            │       │
//	│  │ SPA fallback        │        │                     │       │
//	│  └─────────────────────┘        └─────────────────────┘       │
//	│                                                               │
//	├───────────────────────────────────────────────────────────────┤
//	│                       Middleware Stack                        │
//	│  ┌─────────────────────────────────────────────────────────┐  │
//	│  │  Logger (request/response logging)                      │  │
//	│  │  Recovery (panic recovery with zap logging)             │  │
//	│  └─────────────────────────────────────────────────────────┘  │
//	├───────────────────────────────────────────────────────────────┤
//	│                       Router (/api/v2)                        │
//	│  ┌─────────────────────────────────────────────────────────┐  │
//	│  │  Handlers (registered via callback)                     │  │
//	│  └─────────────────────────────────────────────────────────┘  │
//	└───────────────────────────────────────────────────────────────┘
//
// # Server Modes
//
// Development Mode (ServerMode = "dev"):
//   - HTTP only (no TLS)
//   - Gin runs in debug mode
//   - API endpoints only
//
// Production Mode (ServerMode = "prod"):
//   - HTTPS with auto-generated self-signed certificate (1 year validity)
//   - Gin runs in release mode
//   - Optional static file serving from StaticsFolder
//   - With static files, non-API routes serve index.html as an SPA fallback
//   - With static files, API 404s return JSON error responses
//
// # Server Lifecycle
//
// Creation:
//
//	srv, err := server.NewServer(cfg.Server, map[string]server.APIGroup{
//	    "/api/v2": {
//	        RegisterFn: func(router *gin.RouterGroup) {
//	            v2.RegisterHandlers(router, handler)
//	        },
//	    },
//	})
//
// Each RegisterFn callback receives a RouterGroup with its configured prefix.
//
// Starting:
//
//	// Blocks until error or shutdown
//	err := srv.Start(ctx)
//
// Start automatically chooses HTTP or HTTPS based on TLS configuration.
//
// Stopping:
//
//	srv.Stop(ctx)
//
// Performs graceful shutdown, waiting for in-flight requests to complete.
//
// # Middleware
//
// The server applies two middleware to all API routes:
//
// Logger Middleware (middlewares.Logger):
//   - Logs request start: method, path, query, IP, user-agent, timestamp
//   - Logs request end: all above + status code, latency
//   - Errors logged separately if present
//   - Uses zap structured logging with "http" logger name
//
// Recovery Middleware (ginzap.RecoveryWithZap):
//   - Recovers from panics in handlers
//   - Logs panic details with stack trace
//   - Returns 500 Internal Server Error
//
// # Static File Serving (Production Only)
//
// In production mode with StaticsFolder configured, the server serves:
//
//	/static/*     → StaticsFolder/
//	/             → StaticsFolder/index.html
//	/favicon.ico  → StaticsFolder/favicon.ico
//	/any/path     → StaticsFolder/index.html (SPA fallback)
//	/api/*        → 404 JSON error (if route not found)
//
// # TLS Configuration
//
// In production mode, TLS is configured with:
//   - Self-signed certificate generated at startup
//   - RSA private key
//   - 1 year certificate validity
//   - Certificate generated via pkg/certificates
//
// # Usage Example
//
//	cfg := config.Server{
//	    ServerMode:    "prod",
//	    HTTPPort:      8443,
//	    StaticsFolder: "/app/static",
//	}
//
//	srv, err := server.NewServer(cfg, map[string]server.APIGroup{
//	    "/api/v2": {
//	        RegisterFn: func(router *gin.RouterGroup) {
//	            v2.RegisterHandlers(router, myHandler)
//	        },
//	    },
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Start in goroutine
//	go func() {
//	    if err := srv.Start(ctx); err != http.ErrServerClosed {
//	        log.Printf("server error: %v", err)
//	    }
//	}()
//
//	// Graceful shutdown on signal
//	<-shutdownCh
//	srv.Stop(ctx)
package server

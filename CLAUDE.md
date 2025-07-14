# CLAUDE.md

## Required Steps

After making code changes, always run:
- `just format` - Format Go code using goimports and gofumpt
- `just test` - Run all tests to ensure nothing is broken

## Common Development Commands

### Building and Running
- `just build` - Build the proxy binary for local use
- `just run` - Run the proxy locally on port 9877
- `just build-worker` - Build for Cloudflare Workers deployment
- `just wrangler-dev` - Run local development server with Wrangler

### Development
- `just test` - Run all tests
- `go test ./internal/server -v` - Run specific package tests with verbose output

## High-Level Architecture

This is a Go proxy server that transforms standard Gemini API requests into Google's internal CloudCode format used by Gemini Code Assist API.

### Core Components

#### Server (`internal/server/`)
- **server.go** - Main HTTP server with routing, middleware, and request handling
- **transform.go** - Core transformation logic between Gemini API ↔ CloudCode API
- **admin.go** - Admin API middleware for credential management (protected by ADMIN_API_KEY)
- **http_client*.go** - HTTP client abstractions (separate Workers vs default implementations)

#### Credentials (`internal/credentials/`)
- **provider.go** - Interface for credential management
- **file_provider.go** - File-based credentials (local development)
- **cloudflare_kv_provider.go** - KV-based credentials (Workers deployment)
- Auto-handles OAuth token refresh when expired

#### Environment (`internal/env/`)
- **env.go** - Environment variable access for standard Go
- **env_workers.go** - Environment variable access for Workers runtime

### Dual Deployment Architecture

The codebase supports two deployment modes:

1. **Local/Traditional** (`cmd/gemini-cli-proxy/`) - Uses FileProvider for credentials
2. **Cloudflare Workers** (`cmd/gemini-proxy-worker/`) - Uses CloudflareKVProvider for credentials

### Key Transformations

1. **URL Rewriting**: `/v1beta/models/MODEL:ACTION` → `/v1internal:ACTION`
2. **Model Normalization**: Any model containing "pro"→"gemini-2.5-pro", "flash"→"gemini-2.5-flash"
3. **Request Wrapping**: Standard Gemini requests wrapped in CloudCode format with project ID
4. **Response Unwrapping**: CloudCode responses unwrapped from "response" field
5. **SSE Streaming**: Real-time transformation of Server-Sent Events for streaming responses

### Authentication Flow

- Uses OAuth credentials (access_token, refresh_token) to authenticate with CloudCode API
- Automatically refreshes expired tokens using refresh_token
- For Workers: Admin API allows secure credential upload/management
- Supports both environment-provided project ID or auto-discovery via CloudCode API

### Workers-Specific Considerations

- Cannot access filesystem - uses CloudflareKVProvider instead of FileProvider
- HTTP client uses Workers-compatible fetch API (`github.com/syumai/workers`)
- Graceful fallback for missing http.Flusher support in streaming responses
- Admin API required for credential management (no file access)

## Important Patterns

- All logging uses zerolog (`internal/logger`) with structured logging
- Environment variables handled through `internal/env` abstraction
- Credential providers implement common interface for different storage backends
- Server supports both regular JSON and SSE streaming responses
- Middleware applied to all routes, including main proxy endpoint

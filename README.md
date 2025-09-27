# Gemini Code Assist Proxy

A Go proxy server that transforms standard Gemini API requests into the format expected by Google's Gemini Code Assist API (`cloudcode-pa.googleapis.com`), enabling standard Gemini clients to work with the Code Assist backend.

To run locally, or to deploy to Cloudflare Workers

## What it does

- Transforms `generateContent` and `streamGenerateContent` requests/responses
- Transforms `countTokens` requests/responses
- Supports streaming responses and tool calls
- Normalizes model names: any model with "pro" → `gemini-2.5-pro`, any with "flash" → `gemini-2.5-flash`, any with "lite" → `gemini-2.5-flash-lite`

## Prerequisites

- Go 1.19 or later
- Valid OAuth credentials for Gemini Code Assist API (stored in `~/.gemini/oauth_creds.json`)
- Google Cloud Project ID with Gemini Code Assist API access

## Installation

### Local Development

```
go install github.com/dvcrn/gemini-code-assist-proxy/cmd/gemini-code-assist-proxy@latest
```

Then to start:

```
gemini-code-assist-proxy
```

### Cloudflare Workers Deployment

For production deployment on Cloudflare Workers:

1. **Create KV namespace** (required for credential storage):
   ```bash
   wrangler kv namespace create "gemini-code-assist-proxy-kv"
   ```

   This will output a namespace ID. Add it to your `wrangler.toml`:
   ```toml
   kv_namespaces = [
     { binding = "gemini_code_assist_proxy_kv", id = "YOUR_NAMESPACE_ID_HERE" }
   ]
   ```

2. **Build for Workers**:
   ```bash
   just build-worker
   ```

3. **Deploy to Cloudflare**:
   ```bash
   wrangler deploy
   ```

4. **Set up Admin API Key** (required for credential management):
   ```bash
   # Generate a secure admin key (alphanumeric only, URL-safe)
   head -c 32 /dev/urandom | base64 | tr -d "=+/" | tr -d "\n" | head -c 32

   # Store it as a secret in Workers
   wrangler secret put ADMIN_API_KEY
   ```

5. **Upload OAuth credentials** (see [Admin API](#admin-api) section below)

## Development

```bash
# Clone the repository
git clone <repository-url>
cd gemini-proxy

# Build the proxy
just build

# Or run directly
just run
```

## Configuration

The proxy supports two main authentication methods, with the following order of precedence:

1.  **GCP Project ID**: If you set the `CLOUDCODE_GCP_PROJECT_ID` environment variable, the proxy will use this ID for all requests. This method is suitable for users who want to use a specific GCP project.
2.  **OAuth Credentials (Automatic Discovery)**: If `CLOUDCODE_GCP_PROJECT_ID` is not set, the proxy will attempt to automatically discover a project ID using your OAuth credentials. It loads credentials in the following order:
    *   **`CLOUDCODE_OAUTH_CREDS_PATH`**: The file path to your `oauth_creds.json` file.
    *   **Default Location**:
        *   `~/.gemini/oauth_creds.json` (the default Gemini CLI location)
    *   **`CLOUDCODE_OAUTH_CREDS`**: The raw JSON content of your credentials.
3. Set an `ADMIN_API_KEY` environment variable and set your IDE or editor to pass it along as Gemini API Key

### Environment Variables

Configure the proxy using environment variables:

| Variable | Description | Default | Workers Setup |
| :--- | :--- | :--- | :--- |
| `PORT` | The port for the proxy server. | `9877` | Not applicable |
| `CLOUDCODE_GCP_PROJECT_ID` | The Google Cloud Project ID. | (none) | `wrangler secret put CLOUDCODE_GCP_PROJECT_ID` |
| `ADMIN_API_KEY` | Secure key for protecting admin endpoints | (none) | `wrangler secret put ADMIN_API_KEY` |
| `CLOUDCODE_OAUTH_CREDS_PATH` | Path to the `oauth_creds.json` file. | (none) | Use Admin API instead |
| `CLOUDCODE_OAUTH_CREDS` | Raw JSON content of the credentials. | (none) | Use Admin API instead |
| `SSE_BUFFER_SIZE` | Buffer size for SSE streaming pipeline | `3` | Environment variable |
| `DEBUG_SSE` | Enable detailed SSE event logging | `false` | Environment variable |

**Note**: For Cloudflare Workers deployment, OAuth credentials are managed via the Admin API instead of environment variables or files.

## Admin API

The Admin API provides secure endpoints for managing OAuth credentials. This is essential for deployments that don't have access to the local filesystem.

### Authentication

All admin endpoints require authentication via one of these methods:

- `Authorization: Bearer YOUR_ADMIN_API_KEY` header
- `key=YOUR_ADMIN_API_KEY` query parameter

**Security Note**: The admin API key prevents unauthorized access to credential management endpoints. Keep this key secure and never commit it to version control.

### Endpoints

#### POST /admin/credentials

Updates OAuth credentials stored in Cloudflare KV. Accepts the exact same JSON format as `~/.gemini/oauth_creds.json`:

```bash
curl -X POST https://your-worker.workers.dev/admin/credentials \
  -H "Authorization: Bearer YOUR_ADMIN_API_KEY" \
  -H "Content-Type: application/json" \
  -d @~/.gemini/oauth_creds.json
```

**Response**:
```json
{
  "success": true,
  "message": "Credentials saved successfully"
}
```

#### GET /admin/credentials/status

Check the status of stored OAuth credentials:

```bash
curl https://your-worker.workers.dev/admin/credentials/status \
  -H "Authorization: Bearer YOUR_ADMIN_API_KEY"
```

**Response**:
```json
{
  "type": "oauth",
  "hasCredentials": true,
  "provider": "CloudflareKVProvider",
  "is_expired": false,
  "expiry_date": 1752516043000,
  "expiry_date_formatted": "2025-07-14T17:53:04Z",
  "has_refresh_token": true
}
```

### Complete Workers Setup Workflow

1. **Generate and set admin key**:
   ```bash
   # Generate admin key (alphanumeric only, URL-safe)
   ADMIN_KEY=$(head -c 32 /dev/urandom | base64 | tr -d "=+/" | tr -d "\n" | head -c 32)
   echo "Generated admin key: $ADMIN_KEY"

   # Set it in Workers
   wrangler secret put ADMIN_API_KEY
   ```

2. **Upload OAuth credentials**:
   ```bash
   # Replace with your actual worker URL and admin key
   WORKER_URL="https://your-worker.workers.dev"
   ADMIN_KEY="YOUR_ADMIN_API_KEY"

   # Upload credentials from local file
   curl -X POST $WORKER_URL/admin/credentials \
     -H "Authorization: Bearer $ADMIN_KEY" \
     -H "Content-Type: application/json" \
     -d @~/.gemini/oauth_creds.json
   ```

3. **Verify credentials**:
   ```bash
   # Check credential status
   curl $WORKER_URL/admin/credentials/status \
     -H "Authorization: Bearer $ADMIN_KEY"
   ```

## Core Transformations

### 1. URL Path Rewriting

Transforms standard Gemini API paths to Gemini Code Assist's internal format:

- **From:** `/v1beta/models/gemini-1.5-pro:generateContent`
- **To:** `/v1internal:generateContent`

### 2. Model Normalization

Automatically converts model names to Gemini Code Assist's supported models:

- Any model containing "pro" → `gemini-2.5-pro`
- Any model containing "flash" → `gemini-2.5-flash`
- Any model containing "lite" → `gemini-2.5-flash-lite`

Examples:
- `gemini-1.5-pro` → `gemini-2.5-pro`
- `gemini-1.5-flash` → `gemini-2.5-flash`
- `gemini-pro-latest` → `gemini-2.5-pro`

### 3. Request Body Transformation

#### For generateContent/streamGenerateContent:

**Standard Gemini Request:**
```json
{
  "contents": [{
    "parts": [{ "text": "Why is the sky blue?" }]
  }]
}
```

**Transformed Code Assist Request:**
```json
{
  "model": "gemini-2.5-pro",
  "project": "your-project-id",
  "request": {
    "contents": [{
      "parts": [{ "text": "Why is the sky blue?" }]
    }]
  }
}
```

#### For countTokens:

**Standard Gemini Request:**
```json
{
  "contents": [{
    "parts": [{ "text": "Count these tokens" }]
  }]
}
```

**Transformed Code Assist Request:**
```json
{
  "request": {
    "model": "models/gemini-2.5-pro",
    "contents": [{
      "parts": [{ "text": "Count these tokens" }]
    }]
  }
}
```

### 4. Response Transformation

Code Assist wraps responses in a `response` field which the proxy automatically unwraps:

**Code Assist Response:**
```json
{
  "response": {
    "candidates": [...],
    "promptFeedback": {...}
  }
}
```

**Transformed to Standard Gemini Response:**
```json
{
  "candidates": [...],
  "promptFeedback": {...}
}
```

## Usage Examples

### Basic Generation Request

```bash
curl -X POST 'http://localhost:9877/v1beta/models/gemini-1.5-pro:generateContent?key=DUMMY_KEY' \
-H 'Content-Type: application/json' \
-d '{
  "contents": [
    { "parts": [{ "text": "Why is the sky blue?" }] }
  ]
}'
```

### Streaming Request

```bash
curl -X POST 'http://localhost:9877/v1beta/models/gemini-1.5-flash:streamGenerateContent?key=DUMMY_KEY' \
-H 'Content-Type: application/json' \
-d '{
  "contents": [
    { "parts": [{ "text": "Write a haiku about proxies" }] }
  ]
}'
```

### Count Tokens Request

```bash
curl -X POST 'http://localhost:9877/v1beta/models/gemini-1.5-pro:countTokens?key=DUMMY_KEY' \
-H 'Content-Type: application/json' \
-d '{
  "contents": [
    { "parts": [{ "text": "How many tokens is this?" }] }
  ]
}'
```

## Performance Tuning

### SSE Streaming Optimization

The proxy uses a goroutine pipeline for efficient SSE streaming:

1. **Reader goroutine**: Reads from Code Assist response
2. **Transformer goroutine**: Transforms Code Assist SSE to Gemini format
3. **Writer goroutine**: Writes to client with immediate flushing

Tune the pipeline buffer size with `SSE_BUFFER_SIZE` (default: 3).

### Connection Pooling

The proxy maintains persistent HTTP/2 connections to Code Assist:
- Max idle connections: 100
- Max idle connections per host: 10
- Idle connection timeout: 90 seconds

## Troubleshooting

### Code Assist Response Delays

Code Assist can take 7+ seconds to start streaming responses. This is normal behavior from the Code Assist API, not a proxy issue. Enable debug logging to see detailed timing:

```bash
export DEBUG_SSE=true
```

### Authentication Errors

If you receive 401 errors:
1. Check that `CLOUDCODE_OAUTH_CREDS` contains valid OAuth tokens
2. Refresh your OAuth tokens if they've expired
3. Ensure your GCP project has Gemini Code Assist API access

### Debugging

Enable detailed logging:
```bash
export DEBUG_SSE=true  # Show SSE event timing
```

Check logs for:
- Request transformation details
- Code Assist response times
- SSE event delivery timing

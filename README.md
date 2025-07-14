# Gemini CLI API Proxy

A Go proxy server that transforms standard Gemini API requests into the format expected by Google's Gemini Code Assist API (`cloudcode-pa.googleapis.com`), enabling standard Gemini clients to work with the Code Assist backend.

## What it does

- Transforms `generateContent` and `streamGenerateContent` requests/responses
- Transforms `countTokens` requests/responses
- Supports streaming responses and tool calls
- Normalizes model names: any model with "pro" → `gemini-2.5-pro`, any with "flash" → `gemini-2.5-flash`

## Prerequisites

- Go 1.19 or later
- Valid OAuth credentials for Gemini Code Assist API (stored in `~/.gemini/oauth_creds.json`)
- Google Cloud Project ID with Gemini Code Assist API access

## Installation

```
go install github.com/dvcrn/gemini-cli-proxy/cmd/gemini-cli-proxy@latest
```

Then to start:

```
gemini-cli-proxy
```

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

Configure the proxy using environment variables:

| Variable | Description | Default |
| :--- | :--- | :--- |
| `PORT` | The port for the proxy server. | `9877` |
| `CLOUDCODE_GCP_PROJECT_ID` | The Google Cloud Project ID. | (none) |
| `CLOUDCODE_OAUTH_CREDS_PATH` | Path to the `oauth_creds.json` file. | (none) |
| `CLOUDCODE_OAUTH_CREDS` | Raw JSON content of the credentials. | (none) |
| `SSE_BUFFER_SIZE` | Buffer size for SSE streaming pipeline | `3` |
| `DEBUG_SSE` | Enable detailed SSE event logging | `false` |

## Core Transformations

### 1. URL Path Rewriting

Transforms standard Gemini API paths to Gemini Code Assist's internal format:

- **From:** `/v1beta/models/gemini-1.5-pro:generateContent`
- **To:** `/v1internal:generateContent`

### 2. Model Normalization

Automatically converts model names to Gemini Code Assist's supported models:

- Any model containing "pro" → `gemini-2.5-pro`
- Any model containing "flash" → `gemini-2.5-flash`

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

## TODO

- [ ] Automatic OAuth token refresh

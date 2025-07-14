# Gemini API Proxy for CloudCode

A Go proxy server that transforms standard Gemini API requests into CloudCode format (`cloudcode-pa.googleapis.com`), enabling standard Gemini clients to work with Google's Gemini Code Assist backend.

## Features

- **Model Normalization**: Any model with "pro" → `gemini-2.5-pro`, any with "flash" → `gemini-2.5-flash`
- **SSE Streaming**: Real-time streaming with goroutine pipelines
- **OAuth2 Authentication**: Uses local credentials from `~/.gemini/oauth_creds.json`

## Prerequisites

- Go 1.19 or later
- Valid OAuth credentials for CloudCode API (stored in `~/.gemini/oauth_creds.json`)
- Google Cloud Project ID with CloudCode API access

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

Configure the proxy using environment variables:

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `PORT` | The port for the proxy server | `9877` | No |
| `CLOUDCODE_GCP_PROJECT_ID` | Google Cloud Project ID for CloudCode API | - | **Yes** |
| `CLOUDCODE_OAUTH_CREDS` | OAuth credentials JSON content | - | **Yes** |
| `SSE_BUFFER_SIZE` | Buffer size for SSE streaming pipeline | `3` | No |
| `DEBUG_SSE` | Enable detailed SSE event logging | `false` | No |

### Setting up OAuth Credentials

The proxy expects OAuth credentials to be available. Set up your credentials:

1. Ensure your OAuth credentials are stored in `~/.gemini/oauth_creds.json`
2. The `.envrc` file will automatically load them:
   ```bash
   export CLOUDCODE_OAUTH_CREDS=$(cat ~/.gemini/oauth_creds.json)
   ```

## Core Transformations

### 1. URL Path Rewriting

Transforms standard Gemini API paths to CloudCode's internal format:

- **From:** `/v1beta/models/gemini-1.5-pro:generateContent`
- **To:** `/v1internal:generateContent`

### 2. Model Normalization

Automatically converts model names to CloudCode's supported models:

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

**Transformed CloudCode Request:**
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

**Transformed CloudCode Request:**
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

CloudCode wraps responses in a `response` field which the proxy automatically unwraps:

**CloudCode Response:**
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

1. **Reader goroutine**: Reads from CloudCode response
2. **Transformer goroutine**: Transforms CloudCode SSE to Gemini format
3. **Writer goroutine**: Writes to client with immediate flushing

Tune the pipeline buffer size with `SSE_BUFFER_SIZE` (default: 3).

### Connection Pooling

The proxy maintains persistent HTTP/2 connections to CloudCode:
- Max idle connections: 100
- Max idle connections per host: 10
- Idle connection timeout: 90 seconds

## Troubleshooting

### CloudCode Response Delays

CloudCode can take 7+ seconds to start streaming responses. This is normal behavior from the CloudCode API, not a proxy issue. Enable debug logging to see detailed timing:

```bash
export DEBUG_SSE=true
```

### Authentication Errors

If you receive 401 errors:
1. Check that `CLOUDCODE_OAUTH_CREDS` contains valid OAuth tokens
2. Refresh your OAuth tokens if they've expired
3. Ensure your GCP project has CloudCode API access

### Debugging

Enable detailed logging:
```bash
export DEBUG_SSE=true  # Show SSE event timing
```

Check logs for:
- Request transformation details
- CloudCode response times
- SSE event delivery timing

## TODO

- [ ] Automatic OAuth token refresh

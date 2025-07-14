# API Proxy for Gemini Code Assist

This is a simple proxy server that transforms standard Gemini API requests into the format expected by the Gemini Code Assist backend (`cloudcode-pa.googleapis.com`).

This allows you to use standard Gemini clients and tools through Gemini CLI

## Core Transformation

The proxy performs two main transformations on the fly:

**1. URL Rewriting**

It changes the public-facing Gemini URL structure to the internal one.

*   **From:** `/v1beta/models/gemini-1.5-pro:generateContent`
*   **To:** `/v1internal:generateContent`

**2. Request Body Wrapping**

It wraps the standard Gemini request payload inside a CloudCode-specific structure.

*   **From (Standard Gemini):**
    ```json
    {
      "contents": [{ "parts": [{ "text": "Why is the sky blue?" }] }]
    }
    ```
*   **To (CloudCode API):**
    ```json
    {
      "model": "gemini-2.5-pro",
      "project": "xxx",
      "request": {
        "contents": [{ "parts": [{ "text": "Why is the sky blue?" }] }]
      }
    }
    ```

## How to Run

1.  **Build the proxy:**
    ```bash
    just build
    ```
2.  **Run the proxy:**
    ```bash
    just run
    ```
    The proxy will start on port `9877` by default.

## Configuration

Configure the proxy using environment variables:

| Variable            | Description                            | Default                       |
| ------------------- | -------------------------------------- | ----------------------------- |
| `PORT`              | The port for the proxy server.         | `9877`                        |
| `CLOUDCODE_GCP_PROJECT_ID` | The Google Cloud Project ID.           | `xxx`               |
| `CLOUDCODE_OAUTH_CREDS`   | Content of the ~/.gemini/oauth_creds.json file    | `{  }`  |

## Usage Example

Send a standard Gemini API request to the proxy's address. The `key` parameter is required but its value is ignored.

```bash
curl -X POST 'http://localhost:9877/v1beta/models/gemini-1.5-pro:generateContent?key=DUMMY_KEY' \
-H 'Content-Type: application/json' \
-d '{
  "contents": [
    { "parts": [{ "text": "Why is the sky blue?" }] }
  ]
}'
```

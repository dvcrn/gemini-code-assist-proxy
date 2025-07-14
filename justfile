# Build the proxy
build:
    go build -o gemini-proxy ./cmd/gemini-cli-proxy

# Run the proxy
run:
    go run ./cmd/gemini-cli-proxy

# Run all tests
test:
    go test -v ./...

format:
    @echo "Formatting Go code..."
    go tool golang.org/x/tools/cmd/goimports -w .
    go tool mvdan.cc/gofumpt -w .
    @echo "All code formatted!"

# Build for Cloudflare Workers
build-worker:
    go run github.com/syumai/workers/cmd/workers-assets-gen -mode=go
    GOOS=js GOARCH=wasm go build -o ./build/app.wasm cmd/gemini-proxy-worker/main.go

# Run wrangler dev
wrangler-dev:
    bunx wrangler dev

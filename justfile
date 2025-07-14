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

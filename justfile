# Run all tests
test:
    go test -v ./...

format:
    @echo "Formatting Go code..."
    go tool golang.org/x/tools/cmd/goimports -w .
    go tool mvdan.cc/gofumpt -w .
    @echo "All code formatted!"
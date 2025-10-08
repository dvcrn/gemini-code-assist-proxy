module github.com/dvcrn/gemini-code-assist-proxy

go 1.24.4

tool (
	golang.org/x/tools/cmd/goimports
	mvdan.cc/gofumpt
)

require (
	github.com/google/uuid v1.6.0
	github.com/rs/zerolog v1.34.0
	github.com/stretchr/testify v1.11.1
	github.com/syumai/workers v0.30.2
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/google/go-cmp v0.6.0 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.19 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	golang.org/x/mod v0.26.0 // indirect
	golang.org/x/sync v0.16.0 // indirect
	golang.org/x/sys v0.34.0 // indirect
	golang.org/x/tools v0.35.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	mvdan.cc/gofumpt v0.8.0 // indirect
)

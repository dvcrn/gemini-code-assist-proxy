package main

import "github.com/dvcrn/gemini-cli-proxy/internal/server"

func main() {
	server.Start(":8083")
}

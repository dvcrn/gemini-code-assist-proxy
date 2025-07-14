package main

import (
	"os"

	"github.com/dvcrn/gemini-cli-proxy/internal/server"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "9877"
	}

	server.Start(":" + port)
}

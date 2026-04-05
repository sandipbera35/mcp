package main

import (
	"log"
	"os"

	"github.com/joho/godotenv"
	"github.com/mark3labs/mcp-go/server"
	"github.com/sandipbera35/mcp/tools"
)

func main() {
	// Setup general logging
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	// Load .env file if it exists
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found or error loading it. Using environment variables.")
	}

	log.Println("Starting MCP Server...")

	// Initialize the MCP Server
	s := server.NewMCPServer(
		"MCPServer",
		"1.0.0",
		server.WithToolCapabilities(true),
		server.WithResourceCapabilities(true, true),
		server.WithLogging(),
		server.WithInstructions("You are a helpful command line assistant. Use the tools to get information. Be concise and to the point. Don't use any other tools. "),
		server.WithRecovery(),
	)

	// Register Tools
	tools.RegisterAll(s)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	// Create and start the SSE server
	sse := server.NewSSEServer(s)
	log.Printf("MCP Server initialized and listening on port %s", port)
	log.Printf("Connect to SSE stream at http://localhost:%s/sse", port)

	if err := sse.Start(":" + port); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

package tools

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RegisterFetchURLTool creates and registers the fetch_url tool
func RegisterFetchURLTool(s *server.MCPServer) {
	tool := mcp.NewTool("fetch_url",
		mcp.WithDescription("Fetches the raw HTML or text content from a given URL."),
		mcp.WithString("url",
			mcp.Required(),
			mcp.Description("The full URL to fetch (e.g., https://example.com)"),
		),
	)

	s.AddTool(tool, fetchURLHandler)
}

func fetchURLHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	url, err := request.RequireString("url")
	if err != nil || url == "" {
		return mcp.NewToolResultError("the 'url' parameter is required and must be a string"), nil
	}

	log.Printf("Fetching URL: %s", url)

	// Setup an HTTP client with a reasonable timeout
	client := &http.Client{
		Timeout: 15 * time.Second,
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		log.Printf("Error creating request: %v", err)
		return mcp.NewToolResultError(fmt.Sprintf("Failed to create request: %v", err)), nil
	}

	// Set a generic user agent to prevent being blocked by simple scrapers blockers
	req.Header.Set("User-Agent", "MCP-Crawler-Server/1.0")

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Error fetching URL: %v", err)
		return mcp.NewToolResultError(fmt.Sprintf("Failed to fetch URL: %v", err)), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return mcp.NewToolResultError(fmt.Sprintf("HTTP request failed with status code: %d", resp.StatusCode)), nil
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Error reading response body: %v", err)
		return mcp.NewToolResultError(fmt.Sprintf("Failed to read response body: %v", err)), nil
	}

	content := string(bodyBytes)

	// Add a little prefix to give context on the returned result
	resultText := fmt.Sprintf("Contents from %s:\n\n%s", url, content)

	// We might want to truncate if the content is astronomically large, but for now we return as is.
	// You can also consider stripping HTML if requested, but raw text is sufficient for an LLM to parse.

	return mcp.NewToolResultText(resultText), nil
}

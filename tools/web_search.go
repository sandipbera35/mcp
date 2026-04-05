package tools

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RegisterWebSearchTool creates and registers the web_search tool
func RegisterWebSearchTool(s *server.MCPServer) {
	tool := mcp.NewTool("web_search",
		mcp.WithDescription("Searches the internet for current events, facts, or news. Use this to find recent information. Returns a list of titles, URLs, and snippets."),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("The search query (e.g., 'latest news on Go 1.22')"),
		),
	)

	s.AddTool(tool, WebSearchHandler)
}

func WebSearchHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	query, err := request.RequireString("query")
	if err != nil || query == "" {
		return mcp.NewToolResultError("the 'query' parameter is required and must be a string"), nil
	}

	log.Printf("Performing web search for: %s", query)

	searchURL := fmt.Sprintf("https://search.yahoo.com/search?p=%s", url.QueryEscape(query))

	// Setup an HTTP client
	client := &http.Client{
		Timeout: 15 * time.Second,
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to create request: %v", err)), nil
	}

	// Set a realistic User-Agent and headers
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")

	resp, err := client.Do(req)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to perform web search: %v", err)), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return mcp.NewToolResultError(fmt.Sprintf("Search engine returned status code: %d", resp.StatusCode)), nil
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to read search results: %v", err)), nil
	}

	content := string(bodyBytes)
	results := parseSearchHTML(content)

	if len(results) == 0 {
		return mcp.NewToolResultText("No results found or search was blocked. Try using the fetch_url tool on a specific website directly."), nil
	}

	// Format results into readable text for the model
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("Web Search Results for '%s':\n\n", query))

	for i, res := range results {
		builder.WriteString(fmt.Sprintf("%d. %s\n", i+1, res.Title))
		builder.WriteString(fmt.Sprintf("   URL: %s\n", res.URL))
		builder.WriteString(fmt.Sprintf("   Snippet: %s\n\n", res.Snippet))
	}

	return mcp.NewToolResultText(builder.String()), nil
}

type SearchResult struct {
	Title   string
	URL     string
	Snippet string
}

func parseSearchHTML(htmlContent string) []SearchResult {
	var results []SearchResult

	// Simple regex patterns to extract results from Yahoo html page.
	resultBlockRegex := regexp.MustCompile(`(?s)<div class="compTitle options-toggle[^>]*>.*?<div class="compText aAbs"[^>]*>.*?</div>`)
	titleRegex := regexp.MustCompile(`(?s)<h3[^>]*>.*?<span[^>]*>(.*?)</span></h3>`)
	urlRegex := regexp.MustCompile(`(?s)<a[^>]+href="([^"]+)"`)
	snippetRegex := regexp.MustCompile(`(?s)<div class="compText aAbs"[^>]*>.*?<p[^>]*>(.*?)</p></div>`)

	blocks := resultBlockRegex.FindAllString(htmlContent, -1)

	for _, block := range blocks {
		var title, link, snippet string

		// Extract Title
		titleMatch := titleRegex.FindStringSubmatch(block)
		if len(titleMatch) >= 2 {
			title = cleanHTML(titleMatch[1])
		}

		// Extract URL
		urlMatch := urlRegex.FindStringSubmatch(block)
		if len(urlMatch) >= 2 {
			link = cleanHTML(urlMatch[1])
			// Decode Yahoo redirect URLs if possible
			if strings.Contains(link, "RU=") {
				parts := strings.Split(link, "RU=")
				if len(parts) == 2 {
					decoded, err := url.QueryUnescape(strings.Split(parts[1], "/R")[0])
					if err == nil {
						link = decoded
					}
				}
			}
		}

		// Extract Snippet
		snippetMatch := snippetRegex.FindStringSubmatch(block)
		if len(snippetMatch) >= 2 {
			snippet = cleanHTML(snippetMatch[1])
		}

		if title != "" && link != "" && snippet != "" {
			// Avoid duplicate results
			isDup := false
			for _, r := range results {
				if r.URL == link {
					isDup = true
					break
				}
			}
			if !isDup {
				results = append(results, SearchResult{
					Title:   title,
					URL:     link,
					Snippet: snippet,
				})
			}
		}
		
		// Limit to top 5 results
		if len(results) >= 5 {
			break
		}
	}

	return results
}

func cleanHTML(s string) string {
	s = regexp.MustCompile(`<[^>]*>`).ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	s = strings.ReplaceAll(s, "&quot;", "\"")
	s = strings.ReplaceAll(s, "&#39;", "'")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.TrimSpace(s)
	return s
}

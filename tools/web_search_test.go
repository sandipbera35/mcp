package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/sandipbera35/mcp/tests"
)

func TestWebSearchHandler(t *testing.T) {
	request := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "web_search",
			Arguments: map[string]interface{}{
				"query": "latest golang news",
			},
		},
	}

	result, err := tests.TestWebSearchHandler(context.Background(), request)
	if err != nil {
		t.Fatalf("webSearchHandler returned error: %v", err)
	}

	if result.IsError {
		errMsg := ""
		if len(result.Content) > 0 {
			if textVal, ok := result.Content[0].(mcp.TextContent); ok {
				errMsg = textVal.Text
			}
		}
		t.Fatalf("result marked as error: %s", errMsg)
	}

	if len(result.Content) == 0 {
		t.Fatalf("expected content, got none")
	}

	textContent, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected text content")
	}

	if !strings.Contains(textContent.Text, "latest golang news") {
		t.Errorf("expected query in result, got: %s", textContent.Text)
	}

	if strings.Contains(textContent.Text, "No results found") {
		t.Errorf("expected search results, got no results: %s", textContent.Text)
	}

	t.Logf("Search Output: %s", textContent.Text)
}

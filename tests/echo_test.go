package tests

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/sandipbera35/mcp/tools"
)

func TestEchoHandler(t *testing.T) {
	request := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "echo",
			Arguments: map[string]any{
				"message": "hello world",
			},
		},
	}

	result, err := tools.EchoHandler(context.Background(), request)
	if err != nil {
		t.Fatalf("echoHandler returned error: %v", err)
	}

	if result.IsError {
		t.Fatalf("result marked as error")
	}

	if len(result.Content) == 0 {
		t.Fatalf("expected content, got none")
	}

	textContent, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected text content")
	}

	if textContent.Text != "Echo: hello world" {
		t.Errorf("expected 'Echo: hello world', got '%s'", textContent.Text)
	}
}

package tests

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/sandipbera35/mcp/config"
	"github.com/sandipbera35/mcp/knowledge"
	"github.com/sandipbera35/mcp/tools"
)

func TestWebSearchHandler(t *testing.T) {
	fixture := `
	<html>
	  <body>
	    <div class="compTitle options-toggle">
	      <h3><span>Go 1.22 Released</span></h3>
	      <a href="https://go.dev/doc/go1.22"></a>
	      <div class="compText aAbs"><p>Official release notes for Go 1.22.</p></div>
	    </div>
	    <div class="compTitle options-toggle">
	      <h3><span>Golang Weekly</span></h3>
	      <a href="https://golangweekly.com"></a>
	      <div class="compText aAbs"><p>Weekly Go ecosystem roundup.</p></div>
	    </div>
	  </body>
	</html>`

	cfg := testConfig(t)
	store, err := knowledge.NewStore(filepath.Join(t.TempDir(), "knowledge.json"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
				Body:       io.NopCloser(strings.NewReader(fixture)),
				Request:    req,
			}, nil
		}),
	}
	handlers := tools.NewHandlersWithClient(cfg, store, client, newFakeVectorStore())
	handlers.SetWebSearchURLTemplate("https://example.test/search?p=%s")

	request := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "web_search",
			Arguments: map[string]any{
				"query": "latest golang news",
			},
		},
	}

	result, err := handlers.WebSearchHandler(context.Background(), request)
	if err != nil {
		t.Fatalf("WebSearchHandler returned error: %v", err)
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
	if !strings.Contains(textContent.Text, "latest golang news") {
		t.Fatalf("expected query in result, got: %s", textContent.Text)
	}
	if !strings.Contains(textContent.Text, "Go 1.22 Released") {
		t.Fatalf("expected parsed search results, got: %s", textContent.Text)
	}
}

func testConfig(t *testing.T) config.Config {
	t.Helper()

	t.Setenv("VECTOR_DB_URL", "http://qdrant.test:6333")
	t.Setenv("WEB_SEARCH_URL_TEMPLATE", "https://example.test/search?p=%s")
	t.Setenv("PUBLIC_BASE_URL", "http://localhost:8080")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.ReadRoot = t.TempDir()
	cfg.KnowledgeStore = filepath.Join(t.TempDir(), "knowledge.json")
	return cfg
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

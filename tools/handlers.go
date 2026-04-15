package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/sandipbera35/mcp/config"
	"github.com/sandipbera35/mcp/knowledge"
	"github.com/sandipbera35/mcp/vector"
)

type Handlers struct {
	cfg        config.Config
	httpClient *http.Client
	knowledge  *knowledge.Store
	vectorDB   vector.Store
	searchURL  string
}

func NewHandlers(cfg config.Config, store *knowledge.Store, vectorDB vector.Store) *Handlers {
	return NewHandlersWithClient(cfg, store, &http.Client{
		Timeout: cfg.HTTPTimeout,
	}, vectorDB)
}

func NewHandlersWithClient(cfg config.Config, store *knowledge.Store, client *http.Client, vectorDB vector.Store) *Handlers {
	if client == nil {
		client = &http.Client{Timeout: cfg.HTTPTimeout}
	}
	return &Handlers{
		cfg:        cfg,
		httpClient: client,
		knowledge:  store,
		vectorDB:   vectorDB,
		searchURL:  cfg.WebSearchURL,
	}
}

func (h *Handlers) SetWebSearchURLTemplate(template string) {
	if strings.TrimSpace(template) != "" {
		h.searchURL = template
	}
}

func (h *Handlers) RegisterAll(s *server.MCPServer) {
	RegisterFetchURLTool(s, h.fetchURLHandler)
	RegisterReadFileTool(s, h.readFileHandler)
	RegisterWriteFileTool(s, h.WriteFileHandler)
	RegisterEditFileTool(s, h.EditFileHandler)
	RegisterEchoTool(s)
	RegisterWebSearchTool(s, h.WebSearchHandler)
	RegisterKnowledgeTools(s, h)
}

func RegisterKnowledgeTools(s *server.MCPServer, h *Handlers) {
	ingest := mcp.NewTool("ingest_knowledge",
		mcp.WithDescription("Adds text, file, or URL content to the configured external vector database for model-agnostic RAG."),
		mcp.WithString("source_type",
			mcp.Required(),
			mcp.Description("One of: text, file, url"),
		),
		mcp.WithString("content", mcp.Description("Inline content when source_type=text")),
		mcp.WithString("path", mcp.Description("File path when source_type=file")),
		mcp.WithString("url", mcp.Description("URL when source_type=url")),
		mcp.WithString("title", mcp.Description("Optional human-readable title")),
		mcp.WithArray("tags", mcp.Description("Optional tags"), mcp.WithStringItems()),
		mcp.WithNumber("chunk_size", mcp.Description("Optional chunk size in characters")),
		mcp.WithNumber("chunk_overlap", mcp.Description("Optional overlap in characters")),
	)
	s.AddTool(ingest, h.IngestKnowledgeHandler)

	search := mcp.NewTool("search_knowledge",
		mcp.WithDescription("Retrieves the most relevant chunks from the configured external vector database."),
		mcp.WithString("query", mcp.Required(), mcp.Description("Search query")),
		mcp.WithNumber("limit", mcp.Description("Maximum number of chunks to return")),
	)
	s.AddTool(search, h.SearchKnowledgeHandler)

	cache := mcp.NewTool("cache_context",
		mcp.WithDescription("Stores reusable curated context for CAG-style fast retrieval."),
		mcp.WithString("key", mcp.Required(), mcp.Description("Stable context key")),
		mcp.WithString("title", mcp.Description("Optional display title")),
		mcp.WithString("content", mcp.Required(), mcp.Description("Context body")),
		mcp.WithArray("tags", mcp.Description("Optional tags"), mcp.WithStringItems()),
		mcp.WithNumber("ttl_hours", mcp.Description("Optional TTL in hours")),
	)
	s.AddTool(cache, h.CacheContextHandler)

	getCached := mcp.NewTool("get_cached_context",
		mcp.WithDescription("Loads a cached context bundle by key."),
		mcp.WithString("key", mcp.Required(), mcp.Description("Context key")),
	)
	s.AddTool(getCached, h.GetCachedContextHandler)

	listCached := mcp.NewTool("list_cached_contexts",
		mcp.WithDescription("Lists matching cached context bundles."),
		mcp.WithString("query", mcp.Description("Optional filter text")),
		mcp.WithNumber("limit", mcp.Description("Maximum number of bundles to return")),
	)
	s.AddTool(listCached, h.ListCachedContextsHandler)
}

func RegisterFetchURLTool(s *server.MCPServer, handler server.ToolHandlerFunc) {
	tool := mcp.NewTool("fetch_url",
		mcp.WithDescription("Fetches a URL with production-safe timeouts and byte limits."),
		mcp.WithString("url",
			mcp.Required(),
			mcp.Description("The full URL to fetch (e.g., https://example.com)"),
		),
	)

	s.AddTool(tool, handler)
}

func RegisterReadFileTool(s *server.MCPServer, handler server.ToolHandlerFunc) {
	tool := mcp.NewTool("read_file",
		mcp.WithDescription("Reads a local text file rooted inside the configured READ_ROOT."),
		mcp.WithString("path",
			mcp.Required(),
			mcp.Description("The absolute or relative path to the file"),
		),
	)

	s.AddTool(tool, handler)
}

func RegisterWriteFileTool(s *server.MCPServer, handler server.ToolHandlerFunc) {
	tool := mcp.NewTool("write_file",
		mcp.WithDescription("Creates or overwrites a local text file rooted inside the configured READ_ROOT."),
		mcp.WithString("path",
			mcp.Required(),
			mcp.Description("The absolute or relative path to the file"),
		),
		mcp.WithString("content",
			mcp.Required(),
			mcp.Description("The full file content to write"),
		),
	)

	s.AddTool(tool, handler)
}

func RegisterEditFileTool(s *server.MCPServer, handler server.ToolHandlerFunc) {
	tool := mcp.NewTool("edit_file",
		mcp.WithDescription("Edits a local text file by replacing text or appending content, rooted inside the configured READ_ROOT."),
		mcp.WithString("path",
			mcp.Required(),
			mcp.Description("The absolute or relative path to the file"),
		),
		mcp.WithString("operation",
			mcp.Required(),
			mcp.Description("One of: replace, append"),
		),
		mcp.WithString("old_text",
			mcp.Description("Text to replace when operation=replace"),
		),
		mcp.WithString("new_text",
			mcp.Required(),
			mcp.Description("Replacement or appended text"),
		),
		mcp.WithBoolean("replace_all",
			mcp.Description("Whether to replace all matches when operation=replace"),
		),
	)

	s.AddTool(tool, handler)
}

func (h *Handlers) fetchURLHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	rawURL, err := request.RequireString("url")
	if err != nil || strings.TrimSpace(rawURL) == "" {
		return mcp.NewToolResultError("the 'url' parameter is required and must be a string"), nil
	}

	parsedURL, err := url.Parse(rawURL)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return mcp.NewToolResultError("invalid URL"), nil
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return mcp.NewToolResultError("only http and https URLs are supported"), nil
	}

	body, contentType, err := h.fetchURL(ctx, rawURL)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	resultText := fmt.Sprintf("Contents from %s\nContent-Type: %s\n\n%s", rawURL, contentType, body)
	return mcp.NewToolResultText(resultText), nil
}

func (h *Handlers) readFileHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	_ = ctx

	path, err := request.RequireString("path")
	if err != nil || strings.TrimSpace(path) == "" {
		return mcp.NewToolResultError("the 'path' parameter is required and must be a non-empty string"), nil
	}

	resolvedPath, err := h.resolveReadPath(path)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	fileInfo, err := os.Stat(resolvedPath)
	if err != nil {
		if os.IsNotExist(err) {
			return mcp.NewToolResultError(fmt.Sprintf("file not found: %s", path)), nil
		}
		return mcp.NewToolResultError(fmt.Sprintf("error checking file stats: %v", err)), nil
	}
	if fileInfo.IsDir() {
		return mcp.NewToolResultError(fmt.Sprintf("the path %s is a directory, not a file", path)), nil
	}
	if fileInfo.Size() > h.cfg.FileMaxBytes {
		return mcp.NewToolResultError(fmt.Sprintf("file is too large to read (limit %d bytes)", h.cfg.FileMaxBytes)), nil
	}

	content, err := os.ReadFile(resolvedPath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to read file: %v", err)), nil
	}

	return mcp.NewToolResultText(string(content)), nil
}

func (h *Handlers) WriteFileHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	_ = ctx

	path, err := request.RequireString("path")
	if err != nil || strings.TrimSpace(path) == "" {
		return mcp.NewToolResultError("the 'path' parameter is required and must be a non-empty string"), nil
	}
	content, err := request.RequireString("content")
	if err != nil {
		return mcp.NewToolResultError("the 'content' parameter is required and must be a string"), nil
	}

	resolvedPath, err := h.resolveWritePath(path)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if int64(len(content)) > h.cfg.FileMaxBytes {
		return mcp.NewToolResultError(fmt.Sprintf("content is too large to write (limit %d bytes)", h.cfg.FileMaxBytes)), nil
	}
	if err := os.MkdirAll(filepath.Dir(resolvedPath), 0o755); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to create parent directory: %v", err)), nil
	}
	if err := os.WriteFile(resolvedPath, []byte(content), 0o644); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to write file: %v", err)), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("File written successfully: %s", resolvedPath)), nil
}

func (h *Handlers) EditFileHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	_ = ctx

	path, err := request.RequireString("path")
	if err != nil || strings.TrimSpace(path) == "" {
		return mcp.NewToolResultError("the 'path' parameter is required and must be a non-empty string"), nil
	}
	operation, err := request.RequireString("operation")
	if err != nil || strings.TrimSpace(operation) == "" {
		return mcp.NewToolResultError("the 'operation' parameter is required and must be a non-empty string"), nil
	}
	newText, err := request.RequireString("new_text")
	if err != nil {
		return mcp.NewToolResultError("the 'new_text' parameter is required and must be a string"), nil
	}

	resolvedPath, err := h.resolveWritePath(path)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	contentBytes, err := os.ReadFile(resolvedPath)
	if err != nil {
		if os.IsNotExist(err) {
			return mcp.NewToolResultError(fmt.Sprintf("file not found: %s", path)), nil
		}
		return mcp.NewToolResultError(fmt.Sprintf("failed to read file for editing: %v", err)), nil
	}

	content := string(contentBytes)
	var updated string

	switch strings.ToLower(strings.TrimSpace(operation)) {
	case "append":
		updated = content + newText
	case "replace":
		oldText, err := request.RequireString("old_text")
		if err != nil || oldText == "" {
			return mcp.NewToolResultError("the 'old_text' parameter is required when operation=replace"), nil
		}
		replaceAll := getBoolArg(request, "replace_all", false)
		if !strings.Contains(content, oldText) {
			return mcp.NewToolResultError("old_text was not found in the file"), nil
		}
		if replaceAll {
			updated = strings.ReplaceAll(content, oldText, newText)
		} else {
			updated = strings.Replace(content, oldText, newText, 1)
		}
	default:
		return mcp.NewToolResultError("unsupported operation: expected replace or append"), nil
	}

	if int64(len(updated)) > h.cfg.FileMaxBytes {
		return mcp.NewToolResultError(fmt.Sprintf("edited file would exceed size limit of %d bytes", h.cfg.FileMaxBytes)), nil
	}
	if err := os.WriteFile(resolvedPath, []byte(updated), 0o644); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to write edited file: %v", err)), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("File edited successfully: %s", resolvedPath)), nil
}

func (h *Handlers) IngestKnowledgeHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sourceType, err := request.RequireString("source_type")
	if err != nil {
		return mcp.NewToolResultError("source_type is required"), nil
	}

	title := request.GetString("title", "")
	tags := getStringSlice(request, "tags")
	chunkSize := getIntFromNumber(request, "chunk_size", h.cfg.DefaultChunkSize)
	chunkOverlap := getIntFromNumber(request, "chunk_overlap", h.cfg.DefaultOverlap)

	input := vector.IngestInput{
		SourceType: strings.ToLower(strings.TrimSpace(sourceType)),
		Title:      title,
		Tags:       tags,
		ChunkSize:  chunkSize,
		Overlap:    chunkOverlap,
	}

	switch input.SourceType {
	case "text":
		content, err := request.RequireString("content")
		if err != nil {
			return mcp.NewToolResultError("content is required when source_type=text"), nil
		}
		input.Text = content
		input.Source = title
	case "file":
		path, err := request.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError("path is required when source_type=file"), nil
		}
		resolvedPath, err := h.resolveReadPath(path)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		content, err := os.ReadFile(resolvedPath)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("read file for ingestion: %v", err)), nil
		}
		input.Text = string(content)
		input.Source = resolvedPath
	case "url":
		rawURL, err := request.RequireString("url")
		if err != nil {
			return mcp.NewToolResultError("url is required when source_type=url"), nil
		}
		content, _, err := h.fetchURL(ctx, rawURL)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		input.Text = content
		input.Source = rawURL
	default:
		return mcp.NewToolResultError("unsupported source_type: expected text, file, or url"), nil
	}

	if h.vectorDB == nil {
		return mcp.NewToolResultError("vector database is not configured"), nil
	}

	document, err := h.vectorDB.Ingest(ctx, input)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("ingest knowledge: %v", err)), nil
	}

	response := fmt.Sprintf("Knowledge ingested.\nDocument ID: %s\nTitle: %s\nChunks: %d\nSource: %s",
		document.DocumentID, document.Title, document.Chunks, document.Source,
	)
	return mcp.NewToolResultText(response), nil
}

func (h *Handlers) SearchKnowledgeHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	_ = ctx

	query, err := request.RequireString("query")
	if err != nil || strings.TrimSpace(query) == "" {
		return mcp.NewToolResultError("query is required"), nil
	}
	if h.vectorDB == nil {
		return mcp.NewToolResultError("vector database is not configured"), nil
	}
	limit := getIntFromNumber(request, "limit", h.cfg.SearchResultLimit)

	results, err := h.vectorDB.Search(ctx, query, limit)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("search knowledge: %v", err)), nil
	}
	if len(results) == 0 {
		return mcp.NewToolResultText("No knowledge matches found."), nil
	}

	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("Knowledge results for %q:\n\n", query))
	for i, result := range results {
		builder.WriteString(fmt.Sprintf("%d. [%s] score=%.2f source=%s\n", i+1, result.Document, result.Score, result.Source))
		builder.WriteString(fmt.Sprintf("   Chunk: %s\n", result.ChunkID))
		builder.WriteString(fmt.Sprintf("   Text: %s\n\n", result.ChunkText))
	}
	return mcp.NewToolResultText(builder.String()), nil
}

func (h *Handlers) CacheContextHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	_ = ctx

	key, err := request.RequireString("key")
	if err != nil {
		return mcp.NewToolResultError("key is required"), nil
	}
	content, err := request.RequireString("content")
	if err != nil {
		return mcp.NewToolResultError("content is required"), nil
	}

	ttlHours := getIntFromNumber(request, "ttl_hours", 0)
	ttl := time.Duration(ttlHours) * time.Hour

	entry, err := h.knowledge.SaveContext(key, request.GetString("title", ""), content, getStringSlice(request, "tags"), nil, ttl)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("cache context: %v", err)), nil
	}

	response := fmt.Sprintf("Context cached.\nKey: %s\nTitle: %s\nTags: %s", entry.Key, entry.Title, strings.Join(entry.Tags, ", "))
	return mcp.NewToolResultText(response), nil
}

func (h *Handlers) GetCachedContextHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	_ = ctx

	key, err := request.RequireString("key")
	if err != nil {
		return mcp.NewToolResultError("key is required"), nil
	}

	entry, ok, err := h.knowledge.GetContext(key)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("get cached context: %v", err)), nil
	}
	if !ok {
		return mcp.NewToolResultText("Cached context not found."), nil
	}

	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("Context: %s\nTitle: %s\nTags: %s\n\n%s",
		entry.Key, entry.Title, strings.Join(entry.Tags, ", "), entry.Content,
	))
	return mcp.NewToolResultText(builder.String()), nil
}

func (h *Handlers) ListCachedContextsHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	_ = ctx

	query := request.GetString("query", "")
	limit := getIntFromNumber(request, "limit", 10)
	entries := h.knowledge.ListContexts(query, limit)
	if len(entries) == 0 {
		return mcp.NewToolResultText("No cached contexts found."), nil
	}

	type summary struct {
		Key        string     `json:"key"`
		Title      string     `json:"title"`
		Tags       []string   `json:"tags"`
		CreatedAt  time.Time  `json:"created_at"`
		ExpiresAt  *time.Time `json:"expires_at,omitempty"`
		UsageCount int        `json:"usage_count"`
	}
	items := make([]summary, 0, len(entries))
	for _, entry := range entries {
		items = append(items, summary{
			Key:        entry.Key,
			Title:      entry.Title,
			Tags:       entry.Tags,
			CreatedAt:  entry.CreatedAt,
			ExpiresAt:  entry.ExpiresAt,
			UsageCount: entry.UsageCount,
		})
	}
	body, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("serialize contexts: %v", err)), nil
	}
	return mcp.NewToolResultText(string(body)), nil
}

func (h *Handlers) fetchURL(ctx context.Context, rawURL string) (string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "ProductionMCP/2.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,text/plain;q=0.8,*/*;q=0.5")
	req.Header.Set("Accept-Language", "en-US,en;q=0.8")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("http request failed with status code: %d", resp.StatusCode)
	}

	reader := io.LimitReader(resp.Body, h.cfg.FetchMaxBytes+1)
	bodyBytes, err := io.ReadAll(reader)
	if err != nil {
		return "", "", fmt.Errorf("failed to read response body: %w", err)
	}
	if int64(len(bodyBytes)) > h.cfg.FetchMaxBytes {
		return "", "", fmt.Errorf("response exceeded byte limit of %d", h.cfg.FetchMaxBytes)
	}

	return string(bodyBytes), resp.Header.Get("Content-Type"), nil
}

func (h *Handlers) resolveReadPath(userPath string) (string, error) {
	if strings.TrimSpace(userPath) == "" {
		return "", fmt.Errorf("path is required")
	}

	candidate := userPath
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(h.cfg.ReadRoot, candidate)
	}

	resolved, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve file path: %w", err)
	}

	rel, err := filepath.Rel(h.cfg.ReadRoot, resolved)
	if err != nil {
		return "", fmt.Errorf("resolve path containment: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %s is outside READ_ROOT %s", userPath, h.cfg.ReadRoot)
	}

	return resolved, nil
}

func (h *Handlers) resolveWritePath(userPath string) (string, error) {
	return h.resolveReadPath(userPath)
}

func getStringSlice(request mcp.CallToolRequest, key string) []string {
	raw, ok := request.GetArguments()[key]
	if !ok {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	values := make([]string, 0, len(items))
	for _, item := range items {
		if value, ok := item.(string); ok {
			values = append(values, value)
		}
	}
	return values
}

func getIntFromNumber(request mcp.CallToolRequest, key string, fallback int) int {
	raw, ok := request.GetArguments()[key]
	if !ok {
		return fallback
	}
	switch value := raw.(type) {
	case int:
		return value
	case int32:
		return int(value)
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		parsed, err := strconv.Atoi(value.String())
		if err == nil {
			return parsed
		}
	case string:
		parsed, err := strconv.Atoi(value)
		if err == nil {
			return parsed
		}
	}
	return fallback
}

func getBoolArg(request mcp.CallToolRequest, key string, fallback bool) bool {
	raw, ok := request.GetArguments()[key]
	if !ok {
		return fallback
	}
	switch value := raw.(type) {
	case bool:
		return value
	case string:
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "true", "1", "yes":
			return true
		case "false", "0", "no":
			return false
		}
	}
	return fallback
}

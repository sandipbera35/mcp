package vector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

type ChromaConfig struct {
	URL        string
	APIKey     string
	Username   string
	Password   string
	Tenant     string
	Database   string
	Collection string
	Dimension  int
}

type ChromaStore struct {
	baseURL      *url.URL
	httpClient   *http.Client
	config       ChromaConfig
	collectionID string
}

func NewChromaStore(cfg ChromaConfig, client *http.Client) (*ChromaStore, error) {
	if strings.TrimSpace(cfg.URL) == "" {
		return nil, fmt.Errorf("vector database url is required")
	}
	if strings.TrimSpace(cfg.Collection) == "" {
		return nil, fmt.Errorf("vector database collection is required")
	}
	if strings.TrimSpace(cfg.Tenant) == "" {
		cfg.Tenant = "default_tenant"
	}
	if strings.TrimSpace(cfg.Database) == "" {
		cfg.Database = "default_database"
	}
	if cfg.Dimension <= 0 {
		cfg.Dimension = 384
	}
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}

	parsedURL, err := url.Parse(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("parse vector database url: %w", err)
	}

	return &ChromaStore{
		baseURL:    parsedURL,
		httpClient: client,
		config:     cfg,
	}, nil
}

func (c *ChromaStore) EnsureCollection(ctx context.Context) error {
	body := map[string]any{
		"name":          c.config.Collection,
		"get_or_create": true,
		"metadata": map[string]any{
			"provider":  "mcp",
			"dimension": c.config.Dimension,
		},
	}

	responseBody, _, err := c.do(ctx, http.MethodPost, c.collectionsPath(), body)
	if err != nil {
		return err
	}

	var response struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return fmt.Errorf("decode chroma collection response: %w", err)
	}
	if response.ID == "" {
		return fmt.Errorf("chroma collection id missing from response")
	}
	c.collectionID = response.ID
	return nil
}

func (c *ChromaStore) Ingest(ctx context.Context, input IngestInput) (IngestResult, error) {
	if err := c.EnsureCollection(ctx); err != nil {
		return IngestResult{}, err
	}

	documentID := newID("doc")
	title := fallbackTitle(input.Title, input.Source)
	chunks := buildChunks(documentID, input.Text, input.ChunkSize, input.Overlap)

	ids := make([]string, 0, len(chunks))
	documents := make([]string, 0, len(chunks))
	embeddings := make([][]float32, 0, len(chunks))
	metadatas := make([]map[string]any, 0, len(chunks))
	for _, chunk := range chunks {
		ids = append(ids, chunk.ID)
		documents = append(documents, chunk.Text)
		embeddings = append(embeddings, EmbedText(chunk.Text, c.config.Dimension))
		metadatas = append(metadatas, map[string]any{
			"document_id":  documentID,
			"document":     title,
			"source":       input.Source,
			"source_type":  input.SourceType,
			"chunk_id":     chunk.ID,
			"chunk_text":   chunk.Text,
			"chunk_number": chunk.Position,
			"tags":         strings.Join(input.Tags, ","),
		})
	}

	body := map[string]any{
		"ids":        ids,
		"documents":  documents,
		"embeddings": embeddings,
		"metadatas":  metadatas,
	}
	if _, _, err := c.do(ctx, http.MethodPost, c.collectionPath("/add"), body); err != nil {
		return IngestResult{}, err
	}

	return IngestResult{
		DocumentID: documentID,
		Title:      title,
		Source:     input.Source,
		Chunks:     len(chunks),
	}, nil
}

func (c *ChromaStore) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	if err := c.EnsureCollection(ctx); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 5
	}

	body := map[string]any{
		"query_embeddings": [][]float32{EmbedText(query, c.config.Dimension)},
		"n_results":        limit,
		"include":          []string{"documents", "metadatas", "distances"},
	}

	responseBody, _, err := c.do(ctx, http.MethodPost, c.collectionPath("/query"), body)
	if err != nil {
		return nil, err
	}

	var response struct {
		Documents [][]string         `json:"documents"`
		Metadatas [][]map[string]any `json:"metadatas"`
		Distances [][]float64        `json:"distances"`
		IDs       [][]string         `json:"ids"`
	}
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return nil, fmt.Errorf("decode chroma query response: %w", err)
	}

	if len(response.Documents) == 0 {
		return nil, nil
	}

	results := make([]SearchResult, 0, len(response.Documents[0]))
	for i := range response.Documents[0] {
		metadata := map[string]any{}
		if len(response.Metadatas) > 0 && len(response.Metadatas[0]) > i {
			metadata = response.Metadatas[0][i]
		}
		score := 0.0
		if len(response.Distances) > 0 && len(response.Distances[0]) > i {
			score = 1 / (1 + response.Distances[0][i])
		}
		chunkID := ""
		if len(response.IDs) > 0 && len(response.IDs[0]) > i {
			chunkID = response.IDs[0][i]
		}
		results = append(results, SearchResult{
			DocumentID:  getStringMetadata(metadata, "document_id"),
			Document:    getStringMetadata(metadata, "document"),
			Source:      getStringMetadata(metadata, "source"),
			ChunkID:     fallbackString(getStringMetadata(metadata, "chunk_id"), chunkID),
			ChunkText:   fallbackString(getStringMetadata(metadata, "chunk_text"), response.Documents[0][i]),
			Tags:        splitTags(getStringMetadata(metadata, "tags")),
			Score:       score,
			ChunkNumber: getIntMetadata(metadata, "chunk_number"),
		})
	}
	return results, nil
}

func (c *ChromaStore) Health(ctx context.Context) error {
	_, _, err := c.do(ctx, http.MethodGet, c.collectionsCountPath(), nil)
	return err
}

func (c *ChromaStore) collectionsPath() string {
	return fmt.Sprintf("/api/v2/tenants/%s/databases/%s/collections", c.config.Tenant, c.config.Database)
}

func (c *ChromaStore) collectionsCountPath() string {
	return fmt.Sprintf("/api/v2/tenants/%s/databases/%s/collections_count", c.config.Tenant, c.config.Database)
}

func (c *ChromaStore) collectionPath(suffix string) string {
	return fmt.Sprintf("/api/v2/tenants/%s/databases/%s/collections/%s%s", c.config.Tenant, c.config.Database, c.collectionID, suffix)
}

func (c *ChromaStore) do(ctx context.Context, method, endpoint string, payload any) ([]byte, int, error) {
	target := *c.baseURL
	target.Path = path.Join(target.Path, endpoint)

	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, 0, fmt.Errorf("encode chroma request: %w", err)
		}
		body = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, target.String(), body)
	if err != nil {
		return nil, 0, fmt.Errorf("build chroma request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.config.APIKey != "" {
		req.Header.Set("x-chroma-token", c.config.APIKey)
	}
	if c.config.Username != "" || c.config.Password != "" {
		req.SetBasicAuth(c.config.Username, c.config.Password)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("chroma request failed: %w", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read chroma response: %w", err)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return responseBody, resp.StatusCode, nil
	}
	return responseBody, resp.StatusCode, fmt.Errorf("chroma returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
}

func getStringMetadata(metadata map[string]any, key string) string {
	value, ok := metadata[key]
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		return fmt.Sprintf("%.0f", typed)
	default:
		return fmt.Sprint(typed)
	}
}

func getIntMetadata(metadata map[string]any, key string) int {
	value, ok := metadata[key]
	if !ok {
		return 0
	}
	switch typed := value.(type) {
	case int:
		return typed
	case float64:
		return int(typed)
	case string:
		var parsed int
		_, _ = fmt.Sscanf(typed, "%d", &parsed)
		return parsed
	default:
		return 0
	}
}

func splitTags(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func fallbackString(primary, secondary string) string {
	if strings.TrimSpace(primary) != "" {
		return primary
	}
	return secondary
}

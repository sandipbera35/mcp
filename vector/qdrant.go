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

type QdrantConfig struct {
	URL        string
	APIKey     string
	Username   string
	Password   string
	Collection string
	Dimension  int
	Distance   string
}

type QdrantStore struct {
	baseURL    *url.URL
	httpClient *http.Client
	config     QdrantConfig
}

func NewQdrantStore(cfg QdrantConfig, client *http.Client) (*QdrantStore, error) {
	if strings.TrimSpace(cfg.URL) == "" {
		return nil, fmt.Errorf("vector database url is required")
	}
	if strings.TrimSpace(cfg.Collection) == "" {
		return nil, fmt.Errorf("vector database collection is required")
	}
	if cfg.Dimension <= 0 {
		cfg.Dimension = 384
	}
	if strings.TrimSpace(cfg.Distance) == "" {
		cfg.Distance = "Cosine"
	}
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}

	parsedURL, err := url.Parse(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("parse vector database url: %w", err)
	}
	return &QdrantStore{
		baseURL:    parsedURL,
		httpClient: client,
		config:     cfg,
	}, nil
}

func (q *QdrantStore) EnsureCollection(ctx context.Context) error {
	_, statusCode, err := q.do(ctx, http.MethodGet, "/collections/"+q.config.Collection, nil)
	if err == nil && statusCode == http.StatusOK {
		return nil
	}
	if statusCode != http.StatusNotFound && statusCode != 0 {
		return err
	}

	body := map[string]any{
		"vectors": map[string]any{
			"size":     q.config.Dimension,
			"distance": q.config.Distance,
		},
	}
	_, _, err = q.do(ctx, http.MethodPut, "/collections/"+q.config.Collection, body)
	return err
}

func (q *QdrantStore) Ingest(ctx context.Context, input IngestInput) (IngestResult, error) {
	if err := q.EnsureCollection(ctx); err != nil {
		return IngestResult{}, err
	}

	documentID := newID("doc")
	title := fallbackTitle(input.Title, input.Source)
	chunks := buildChunks(documentID, input.Text, input.ChunkSize, input.Overlap)
	points := make([]map[string]any, 0, len(chunks))
	for _, chunk := range chunks {
		points = append(points, map[string]any{
			"id":     chunk.ID,
			"vector": EmbedText(chunk.Text, q.config.Dimension),
			"payload": map[string]any{
				"document_id":  documentID,
				"document":     title,
				"source":       input.Source,
				"source_type":  input.SourceType,
				"chunk_id":     chunk.ID,
				"chunk_text":   chunk.Text,
				"chunk_number": chunk.Position,
				"tags":         input.Tags,
			},
		})
	}

	body := map[string]any{"points": points}
	if _, _, err := q.do(ctx, http.MethodPut, "/collections/"+q.config.Collection+"/points?wait=true", body); err != nil {
		return IngestResult{}, err
	}

	return IngestResult{
		DocumentID: documentID,
		Title:      title,
		Source:     input.Source,
		Chunks:     len(chunks),
	}, nil
}

func (q *QdrantStore) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 5
	}
	body := map[string]any{
		"vector":       EmbedText(query, q.config.Dimension),
		"limit":        limit,
		"with_payload": true,
	}

	responseBody, _, err := q.do(ctx, http.MethodPost, "/collections/"+q.config.Collection+"/points/search", body)
	if err != nil {
		return nil, err
	}

	var response struct {
		Result []struct {
			Score   float64       `json:"score"`
			Payload qdrantPayload `json:"payload"`
		} `json:"result"`
	}
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return nil, fmt.Errorf("decode qdrant search response: %w", err)
	}

	results := make([]SearchResult, 0, len(response.Result))
	for _, item := range response.Result {
		results = append(results, SearchResult{
			DocumentID:  item.Payload.DocumentID,
			Document:    item.Payload.Document,
			Source:      item.Payload.Source,
			ChunkID:     item.Payload.ChunkID,
			ChunkText:   item.Payload.ChunkText,
			Tags:        item.Payload.Tags,
			Score:       item.Score,
			ChunkNumber: item.Payload.ChunkNumber,
		})
	}
	return results, nil
}

func (q *QdrantStore) Health(ctx context.Context) error {
	_, _, err := q.do(ctx, http.MethodGet, "/collections/"+q.config.Collection, nil)
	return err
}

type qdrantPayload struct {
	DocumentID  string   `json:"document_id"`
	Document    string   `json:"document"`
	Source      string   `json:"source"`
	ChunkID     string   `json:"chunk_id"`
	ChunkText   string   `json:"chunk_text"`
	Tags        []string `json:"tags"`
	ChunkNumber int      `json:"chunk_number"`
}

type vectorChunk struct {
	ID       string
	Position int
	Text     string
}

func buildChunks(documentID, text string, chunkSize, overlap int) []vectorChunk {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if chunkSize < 200 {
		chunkSize = 900
	}
	if overlap < 0 || overlap >= chunkSize {
		overlap = chunkSize / 6
	}

	runes := []rune(text)
	step := chunkSize - overlap
	if step <= 0 {
		step = chunkSize
	}

	var chunks []vectorChunk
	for start, position := 0, 0; start < len(runes); start, position = start+step, position+1 {
		end := start + chunkSize
		if end > len(runes) {
			end = len(runes)
		}
		chunkText := strings.TrimSpace(string(runes[start:end]))
		if chunkText != "" {
			chunks = append(chunks, vectorChunk{
				ID:       newID("chunk"),
				Position: position,
				Text:     chunkText,
			})
		}
		if end == len(runes) {
			break
		}
	}
	return chunks
}

func fallbackTitle(title, source string) string {
	title = strings.TrimSpace(title)
	if title != "" {
		return title
	}
	source = strings.TrimSpace(source)
	if source != "" {
		return source
	}
	return "untitled"
}

func newID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UTC().UnixNano())
}

func (q *QdrantStore) do(ctx context.Context, method, endpoint string, payload any) ([]byte, int, error) {
	target := *q.baseURL
	target.Path = path.Join(target.Path, endpoint)

	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, 0, fmt.Errorf("encode qdrant request: %w", err)
		}
		body = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, target.String(), body)
	if err != nil {
		return nil, 0, fmt.Errorf("build qdrant request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if q.config.APIKey != "" {
		req.Header.Set("api-key", q.config.APIKey)
	}
	if q.config.Username != "" || q.config.Password != "" {
		req.SetBasicAuth(q.config.Username, q.config.Password)
	}

	resp, err := q.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("qdrant request failed: %w", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read qdrant response: %w", err)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return responseBody, resp.StatusCode, nil
	}
	return responseBody, resp.StatusCode, fmt.Errorf("qdrant returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
}

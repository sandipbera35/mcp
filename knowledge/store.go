package knowledge

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

type Store struct {
	path string
	mu   sync.RWMutex
	data persistedStore
}

type persistedStore struct {
	Documents []Document              `json:"documents"`
	Contexts  map[string]ContextEntry `json:"contexts"`
}

type Document struct {
	ID         string            `json:"id"`
	SourceType string            `json:"source_type"`
	Source     string            `json:"source"`
	Title      string            `json:"title"`
	Tags       []string          `json:"tags"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	Chunks     []Chunk           `json:"chunks"`
	CreatedAt  time.Time         `json:"created_at"`
}

type Chunk struct {
	ID        string `json:"id"`
	Document  string `json:"document"`
	Position  int    `json:"position"`
	Text      string `json:"text"`
	TokenSize int    `json:"token_size"`
}

type ContextEntry struct {
	Key        string            `json:"key"`
	Title      string            `json:"title"`
	Content    string            `json:"content"`
	Tags       []string          `json:"tags"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	CreatedAt  time.Time         `json:"created_at"`
	ExpiresAt  *time.Time        `json:"expires_at,omitempty"`
	UsageCount int               `json:"usage_count"`
}

type IngestInput struct {
	SourceType string
	Source     string
	Title      string
	Tags       []string
	Metadata   map[string]string
	Text       string
	ChunkSize  int
	Overlap    int
}

type SearchResult struct {
	DocumentID  string   `json:"document_id"`
	Document    string   `json:"document_title"`
	Source      string   `json:"source"`
	ChunkID     string   `json:"chunk_id"`
	ChunkText   string   `json:"chunk_text"`
	Tags        []string `json:"tags"`
	Score       float64  `json:"score"`
	ChunkNumber int      `json:"chunk_number"`
}

var tokenRegex = regexp.MustCompile(`[a-zA-Z0-9]+`)

func NewStore(path string) (*Store, error) {
	store := &Store{
		path: path,
		data: persistedStore{
			Documents: []Document{},
			Contexts:  map[string]ContextEntry{},
		},
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create knowledge store directory: %w", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return store, nil
		}
		return nil, fmt.Errorf("read knowledge store: %w", err)
	}

	if len(content) == 0 {
		return store, nil
	}

	if err := json.Unmarshal(content, &store.data); err != nil {
		return nil, fmt.Errorf("parse knowledge store: %w", err)
	}
	if store.data.Contexts == nil {
		store.data.Contexts = map[string]ContextEntry{}
	}
	return store, nil
}

func (s *Store) Ingest(input IngestInput) (Document, error) {
	if strings.TrimSpace(input.Text) == "" {
		return Document{}, fmt.Errorf("document content is empty")
	}

	chunkSize := input.ChunkSize
	if chunkSize < 200 {
		chunkSize = 900
	}

	overlap := input.Overlap
	if overlap < 0 || overlap >= chunkSize {
		overlap = chunkSize / 6
	}

	document := Document{
		ID:         newID("doc"),
		SourceType: input.SourceType,
		Source:     strings.TrimSpace(input.Source),
		Title:      fallbackTitle(input.Title, input.Source),
		Tags:       normalizeTags(input.Tags),
		Metadata:   cloneMap(input.Metadata),
		CreatedAt:  time.Now().UTC(),
	}
	document.Chunks = buildChunks(document.ID, input.Text, chunkSize, overlap)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.data.Documents = append(s.data.Documents, document)
	if err := s.persistLocked(); err != nil {
		return Document{}, err
	}

	return document, nil
}

func (s *Store) Search(query string, limit int) []SearchResult {
	queryTokens := tokenize(query)
	if len(queryTokens) == 0 {
		return nil
	}
	if limit <= 0 {
		limit = 5
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	totalChunks := 0
	docFreq := map[string]int{}
	chunkTokens := map[string][]string{}
	documentsByID := map[string]Document{}

	for _, document := range s.data.Documents {
		documentsByID[document.ID] = document
		for _, chunk := range document.Chunks {
			totalChunks++
			tokens := tokenize(chunk.Text)
			chunkTokens[chunk.ID] = tokens
			seen := map[string]struct{}{}
			for _, token := range tokens {
				if _, ok := seen[token]; ok {
					continue
				}
				seen[token] = struct{}{}
				docFreq[token]++
			}
		}
	}

	var results []SearchResult
	for _, document := range s.data.Documents {
		for _, chunk := range document.Chunks {
			score := scoreChunk(chunkTokens[chunk.ID], queryTokens, docFreq, totalChunks)
			if score <= 0 {
				continue
			}
			results = append(results, SearchResult{
				DocumentID:  document.ID,
				Document:    document.Title,
				Source:      document.Source,
				ChunkID:     chunk.ID,
				ChunkText:   chunk.Text,
				Tags:        append([]string(nil), document.Tags...),
				Score:       score,
				ChunkNumber: chunk.Position,
			})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			if results[i].Document == results[j].Document {
				return results[i].ChunkNumber < results[j].ChunkNumber
			}
			return results[i].Document < results[j].Document
		}
		return results[i].Score > results[j].Score
	})

	if len(results) > limit {
		results = results[:limit]
	}
	return results
}

func (s *Store) SaveContext(key, title, content string, tags []string, metadata map[string]string, ttl time.Duration) (ContextEntry, error) {
	key = normalizeKey(key)
	if key == "" {
		return ContextEntry{}, fmt.Errorf("context key is required")
	}
	if strings.TrimSpace(content) == "" {
		return ContextEntry{}, fmt.Errorf("context content is empty")
	}

	entry := ContextEntry{
		Key:       key,
		Title:     strings.TrimSpace(title),
		Content:   strings.TrimSpace(content),
		Tags:      normalizeTags(tags),
		Metadata:  cloneMap(metadata),
		CreatedAt: time.Now().UTC(),
	}
	if entry.Title == "" {
		entry.Title = key
	}
	if ttl > 0 {
		expiresAt := entry.CreatedAt.Add(ttl)
		entry.ExpiresAt = &expiresAt
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Contexts[key] = entry
	if err := s.persistLocked(); err != nil {
		return ContextEntry{}, err
	}
	return entry, nil
}

func (s *Store) GetContext(key string) (ContextEntry, bool, error) {
	key = normalizeKey(key)

	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.data.Contexts[key]
	if !ok {
		return ContextEntry{}, false, nil
	}
	if expired(entry) {
		delete(s.data.Contexts, key)
		if err := s.persistLocked(); err != nil {
			return ContextEntry{}, false, err
		}
		return ContextEntry{}, false, nil
	}
	entry.UsageCount++
	s.data.Contexts[key] = entry
	if err := s.persistLocked(); err != nil {
		return ContextEntry{}, false, err
	}
	return entry, true, nil
}

func (s *Store) ListContexts(query string, limit int) []ContextEntry {
	if limit <= 0 {
		limit = 10
	}

	query = strings.TrimSpace(strings.ToLower(query))

	s.mu.RLock()
	defer s.mu.RUnlock()

	var entries []ContextEntry
	for _, entry := range s.data.Contexts {
		if expired(entry) {
			continue
		}
		if query == "" || strings.Contains(strings.ToLower(entry.Key), query) || strings.Contains(strings.ToLower(entry.Title), query) || strings.Contains(strings.ToLower(entry.Content), query) {
			entries = append(entries, entry)
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].UsageCount == entries[j].UsageCount {
			return entries[i].CreatedAt.After(entries[j].CreatedAt)
		}
		return entries[i].UsageCount > entries[j].UsageCount
	})
	if len(entries) > limit {
		entries = entries[:limit]
	}
	return entries
}

func buildChunks(documentID, text string, chunkSize, overlap int) []Chunk {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) == 0 {
		return nil
	}

	step := chunkSize - overlap
	if step <= 0 {
		step = chunkSize
	}

	var chunks []Chunk
	position := 0
	for start := 0; start < len(runes); start += step {
		end := start + chunkSize
		if end > len(runes) {
			end = len(runes)
		}

		chunkText := strings.TrimSpace(string(runes[start:end]))
		if chunkText != "" {
			chunks = append(chunks, Chunk{
				ID:        newID("chunk"),
				Document:  documentID,
				Position:  position,
				Text:      chunkText,
				TokenSize: len(tokenize(chunkText)),
			})
			position++
		}

		if end == len(runes) {
			break
		}
	}

	return chunks
}

func tokenize(value string) []string {
	raw := tokenRegex.FindAllString(strings.ToLower(value), -1)
	if len(raw) == 0 {
		return nil
	}
	return raw
}

func scoreChunk(tokens, queryTokens []string, docFreq map[string]int, totalChunks int) float64 {
	if len(tokens) == 0 || len(queryTokens) == 0 || totalChunks == 0 {
		return 0
	}

	tf := map[string]int{}
	for _, token := range tokens {
		tf[token]++
	}

	var score float64
	for _, token := range queryTokens {
		freq := tf[token]
		if freq == 0 {
			continue
		}
		idf := math.Log(1 + float64(totalChunks)/(1+float64(docFreq[token])))
		score += (1 + math.Log(float64(freq))) * idf
	}
	return score
}

func expired(entry ContextEntry) bool {
	return entry.ExpiresAt != nil && time.Now().UTC().After(*entry.ExpiresAt)
}

func normalizeTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	set := map[string]struct{}{}
	var normalized []string
	for _, tag := range tags {
		tag = strings.TrimSpace(strings.ToLower(tag))
		if tag == "" {
			continue
		}
		if _, ok := set[tag]; ok {
			continue
		}
		set[tag] = struct{}{}
		normalized = append(normalized, tag)
	}
	sort.Strings(normalized)
	return normalized
}

func normalizeKey(key string) string {
	key = strings.TrimSpace(strings.ToLower(key))
	key = strings.ReplaceAll(key, " ", "-")
	return key
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

func cloneMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func (s *Store) persistLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create store directory: %w", err)
	}

	content, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal store: %w", err)
	}
	if err := os.WriteFile(s.path, content, 0o644); err != nil {
		return fmt.Errorf("write store: %w", err)
	}
	return nil
}

func newID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UTC().UnixNano())
}

package chroma

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	v2 "github.com/amikos-tech/chroma-go/pkg/api/v2"
	"github.com/amikos-tech/chroma-go/pkg/embeddings"
	"github.com/amikos-tech/chroma-go/pkg/embeddings/ort"
)

// Record represents a memory entry stored in ChromaDB.
type Record struct {
	ID       string            `json:"id"`
	Content  string            `json:"content"`
	Metadata map[string]string `json:"metadata,omitempty"`
	Distance *float64          `json:"distance,omitempty"`
}

// Store wraps an embedded ChromaDB PersistentClient for a single persistence directory.
type Store struct {
	PersistDir string
	client     v2.Client
	ef         embeddings.EmbeddingFunction
	closeEF    func() error
	mu         sync.Mutex
}

// Open initializes a persistent ChromaDB client and local ONNX embedding function at persistDir.
func Open(persistDir string) (*Store, error) {
	absDir := absOrExpand(persistDir, "")
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		return nil, fmt.Errorf("create persist dir %s: %w", absDir, err)
	}

	ef, closeEF, err := ort.NewDefaultEmbeddingFunction()
	if err != nil {
		return nil, fmt.Errorf("init local ONNX embedding function: %w", err)
	}

	client, err := v2.NewPersistentClient(
		v2.WithPersistentPath(absDir),
	)
	if err != nil {
		if closeEF != nil {
			_ = closeEF()
		}
		return nil, fmt.Errorf("open persistent chroma client at %s: %w", absDir, err)
	}

	return &Store{
		PersistDir: absDir,
		client:     client,
		ef:         ef,
		closeEF:    closeEF,
	}, nil
}

// Close releases the Chroma client and ONNX runtime resources.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client == nil {
		return nil
	}
	err := s.client.Close()
	s.client = nil
	if s.closeEF != nil {
		_ = s.closeEF()
		s.closeEF = nil
	}
	return err
}

func (s *Store) collection(ctx context.Context, collectionName string) (v2.Collection, error) {
	name := strings.TrimSpace(collectionName)
	if name == "" {
		name = DefaultCollectionName()
	}
	col, err := s.client.GetOrCreateCollection(
		ctx,
		name,
		v2.WithEmbeddingFunctionCreate(s.ef),
	)
	if err != nil {
		return nil, fmt.Errorf("get or create collection %q: %w", name, err)
	}
	return col, nil
}

// Save upserts a memory document into ChromaDB and returns its ID.
func (s *Store) Save(ctx context.Context, collectionName, id, content string, meta map[string]string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	docID := strings.TrimSpace(id)
	if docID == "" {
		docID = newUUID()
	}

	col, err := s.collection(ctx, collectionName)
	if err != nil {
		return "", err
	}

	opts := []v2.CollectionAddOption{
		v2.WithIDs(v2.DocumentID(docID)),
		v2.WithTexts(content),
	}
	if len(meta) > 0 {
		raw := make(map[string]any, len(meta))
		for k, v := range meta {
			if strings.TrimSpace(v) != "" {
				raw[k] = v
			}
		}
		if len(raw) > 0 {
			docMeta, err := v2.NewDocumentMetadataFromMap(raw)
			if err != nil {
				return "", fmt.Errorf("build metadata: %w", err)
			}
			opts = append(opts, v2.WithMetadatas(docMeta))
		}
	}

	if err := col.Upsert(ctx, opts...); err != nil {
		return "", fmt.Errorf("upsert memory %q: %w", docID, err)
	}
	return docID, nil
}

// Query performs semantic similarity search in the given collection.
func (s *Store) Query(ctx context.Context, collectionName, query string, nResults int) ([]Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if nResults <= 0 {
		nResults = 5
	}

	col, err := s.collection(ctx, collectionName)
	if err != nil {
		return nil, err
	}

	count, err := col.Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("count collection: %w", err)
	}
	if count == 0 {
		return nil, nil
	}
	if nResults > count {
		nResults = count
	}

	res, err := col.Query(
		ctx,
		v2.WithQueryTexts(query),
		v2.WithNResults(nResults),
		v2.WithInclude(v2.IncludeDocuments, v2.IncludeMetadatas, v2.IncludeDistances),
	)
	if err != nil {
		return nil, fmt.Errorf("query collection: %w", err)
	}

	idGroups := res.GetIDGroups()
	if len(idGroups) == 0 {
		return nil, nil
	}
	ids := idGroups[0]
	var docs v2.Documents
	if docGroups := res.GetDocumentsGroups(); len(docGroups) > 0 {
		docs = docGroups[0]
	}
	var metas v2.DocumentMetadatas
	if metaGroups := res.GetMetadatasGroups(); len(metaGroups) > 0 {
		metas = metaGroups[0]
	}
	var dists embeddings.Distances
	if distGroups := res.GetDistancesGroups(); len(distGroups) > 0 {
		dists = distGroups[0]
	}

	out := make([]Record, 0, len(ids))
	for i, docID := range ids {
		rec := Record{
			ID:       string(docID),
			Metadata: map[string]string{},
		}
		if i < len(docs) && docs[i] != nil {
			rec.Content = docs[i].ContentString()
		}
		if i < len(metas) && metas[i] != nil {
			rec.Metadata = extractStringMetadata(metas[i])
		}
		if i < len(dists) {
			d := float64(dists[i])
			rec.Distance = &d
		}
		out = append(out, rec)
	}
	return out, nil
}

// List returns all stored records in the given collection.
func (s *Store) List(ctx context.Context, collectionName string) ([]Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	col, err := s.collection(ctx, collectionName)
	if err != nil {
		return nil, err
	}

	res, err := col.Get(
		ctx,
		v2.WithInclude(v2.IncludeDocuments, v2.IncludeMetadatas),
	)
	if err != nil {
		return nil, fmt.Errorf("list collection: %w", err)
	}

	ids := res.GetIDs()
	docs := res.GetDocuments()
	metas := res.GetMetadatas()
	out := make([]Record, 0, len(ids))
	for i, docID := range ids {
		rec := Record{
			ID:       string(docID),
			Metadata: map[string]string{},
		}
		if i < len(docs) && docs[i] != nil {
			rec.Content = docs[i].ContentString()
		}
		if i < len(metas) && metas[i] != nil {
			rec.Metadata = extractStringMetadata(metas[i])
		}
		out = append(out, rec)
	}
	return out, nil
}

// Delete removes a memory document by ID.
func (s *Store) Delete(ctx context.Context, collectionName, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	col, err := s.collection(ctx, collectionName)
	if err != nil {
		return err
	}
	if err := col.Delete(ctx, v2.WithIDs(v2.DocumentID(id))); err != nil {
		return fmt.Errorf("delete memory %q: %w", id, err)
	}
	return nil
}

func extractStringMetadata(meta v2.DocumentMetadata) map[string]string {
	out := map[string]string{}
	if meta == nil {
		return out
	}
	if kp, ok := meta.(interface{ Keys() []string }); ok {
		for _, k := range kp.Keys() {
			if v, ok := meta.GetString(k); ok {
				out[k] = v
			} else if v, ok := meta.GetInt(k); ok {
				out[k] = fmt.Sprintf("%d", v)
			} else if v, ok := meta.GetFloat(k); ok {
				out[k] = fmt.Sprintf("%g", v)
			} else if v, ok := meta.GetBool(k); ok {
				out[k] = fmt.Sprintf("%t", v)
			} else if v, ok := meta.GetStringArray(k); ok {
				out[k] = strings.Join(v, ",")
			} else if raw, ok := meta.GetRaw(k); ok && raw != nil {
				out[k] = fmt.Sprintf("%v", raw)
			}
		}
		return out
	}
	for _, k := range []string{"tags", "node_type", "links"} {
		if v, ok := meta.GetString(k); ok {
			out[k] = v
		}
	}
	return out
}

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// Manager caches open Store instances keyed by cleaned persistence path.
type Manager struct {
	mu     sync.Mutex
	stores map[string]*Store
}

// NewManager creates a new Store Manager.
func NewManager() *Manager {
	return &Manager{stores: map[string]*Store{}}
}

// Get opens or returns a cached Store for persistDir.
func (m *Manager) Get(persistDir string) (*Store, error) {
	key := filepath.Clean(absOrExpand(persistDir, ""))

	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.stores[key]; ok {
		return s, nil
	}
	s, err := Open(key)
	if err != nil {
		return nil, err
	}
	m.stores[key] = s
	return s, nil
}

// Close closes all managed stores.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var firstErr error
	for k, s := range m.stores {
		if err := s.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		delete(m.stores, k)
	}
	return firstErr
}

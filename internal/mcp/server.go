package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zerochirou/codegen/internal/chroma"
)

const defaultProtocolVersion = "2024-11-05"

type rpcMessage struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method,omitempty"`
	Params  json.RawMessage  `json:"params,omitempty"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   *rpcError        `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Server implements a stdio MCP server backed by ChromaDB.
type Server struct {
	name    string
	version string
	manager *chroma.Manager

	in      io.Reader
	out     io.Writer
	writeMu sync.Mutex

	rootsMu sync.RWMutex
	roots   []string
	hasRoots bool

	nextReqID atomic.Int64
	pendingMu sync.Mutex
	pending   map[string]chan rpcMessage
}

// NewServer creates a new stdio MCP Server.
func NewServer(name, version string, manager *chroma.Manager) *Server {
	if strings.TrimSpace(name) == "" {
		name = "codegen-mcp"
	}
	if manager == nil {
		manager = chroma.NewManager()
	}
	return &Server{
		name:    name,
		version: version,
		manager: manager,
		in:      os.Stdin,
		out:     os.Stdout,
		pending: map[string]chan rpcMessage{},
	}
}

// Serve runs the JSON-RPC stdio loop until EOF or context cancellation.
func (s *Server) Serve(ctx context.Context) error {
	return s.ServeIO(ctx, os.Stdin, os.Stdout)
}

// ServeIO runs the JSON-RPC loop over the supplied reader and writer (useful for tests).
func (s *Server) ServeIO(ctx context.Context, in io.Reader, out io.Writer) error {
	s.in = in
	s.out = out
	defer func() { _ = s.manager.Close() }()

	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)

	for scanner.Scan() {
		if ctx.Err() != nil {
			break
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var msg rpcMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			s.sendError(nil, -32700, fmt.Sprintf("parse error: %v", err))
			continue
		}

		// Response to a server-initiated request (e.g., roots/list)
		if msg.Method == "" && msg.ID != nil {
			s.deliverResponse(msg)
			continue
		}

		// Notification (no ID)
		if msg.ID == nil {
			s.handleNotification(ctx, msg)
			continue
		}

		// Request (has Method and ID)
		s.handleRequest(ctx, msg)
	}

	return scanner.Err()
}

func (s *Server) handleNotification(ctx context.Context, msg rpcMessage) {
	switch msg.Method {
	case "notifications/initialized", "notifications/roots/list_changed":
		s.rootsMu.RLock()
		supported := s.hasRoots
		s.rootsMu.RUnlock()
		if supported {
			go s.refreshRoots(ctx)
		}
	}
}

func (s *Server) handleRequest(ctx context.Context, req rpcMessage) {
	switch req.Method {
	case "initialize":
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
			Capabilities    struct {
				Roots json.RawMessage `json:"roots"`
			} `json:"capabilities"`
		}
		_ = json.Unmarshal(req.Params, &params)
		proto := params.ProtocolVersion
		if proto == "" {
			proto = defaultProtocolVersion
		}
		if len(params.Capabilities.Roots) > 0 && string(params.Capabilities.Roots) != "null" {
			s.rootsMu.Lock()
			s.hasRoots = true
			s.rootsMu.Unlock()
		}
		s.sendResult(req.ID, map[string]any{
			"protocolVersion": proto,
			"capabilities": map[string]any{
				"tools": map[string]any{
					"listChanged": false,
				},
			},
			"serverInfo": map[string]any{
				"name":    s.name,
				"version": s.version,
			},
		})

	case "ping":
		s.sendResult(req.ID, map[string]any{})

	case "tools/list":
		s.sendResult(req.ID, map[string]any{
			"tools": toolDefinitions(),
		})

	case "tools/call":
		var params struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			s.sendError(req.ID, -32602, fmt.Sprintf("invalid params: %v", err))
			return
		}
		result, err := s.callTool(ctx, params.Name, params.Arguments)
		if err != nil {
			s.sendResult(req.ID, map[string]any{
				"content": []map[string]any{
					{"type": "text", "text": err.Error()},
				},
				"isError": true,
			})
			return
		}
		rawJSON, _ := json.Marshal(result)
		s.sendResult(req.ID, map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": string(rawJSON)},
			},
			"structuredContent": result,
		})

	default:
		s.sendError(req.ID, -32601, fmt.Sprintf("method not found: %s", req.Method))
	}
}

func (s *Server) refreshRoots(ctx context.Context) {
	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	resp, err := s.sendClientRequest(reqCtx, "roots/list", map[string]any{})
	if err != nil || resp.Error != nil {
		return
	}

	var result struct {
		Roots []struct {
			URI string `json:"uri"`
		} `json:"roots"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return
	}

	paths := make([]string, 0, len(result.Roots))
	for _, r := range result.Roots {
		if p := fileURIToPath(r.URI); p != "" {
			paths = append(paths, p)
		}
	}

	s.rootsMu.Lock()
	s.roots = paths
	s.rootsMu.Unlock()
}

func (s *Server) sendClientRequest(ctx context.Context, method string, params any) (rpcMessage, error) {
	idNum := s.nextReqID.Add(1)
	rawID, _ := json.Marshal(idNum)
	key := string(rawID)

	ch := make(chan rpcMessage, 1)
	s.pendingMu.Lock()
	s.pending[key] = ch
	s.pendingMu.Unlock()

	defer func() {
		s.pendingMu.Lock()
		delete(s.pending, key)
		s.pendingMu.Unlock()
	}()

	rawParams, _ := json.Marshal(params)
	msg := rpcMessage{
		JSONRPC: "2.0",
		ID:      (*json.RawMessage)(&rawID),
		Method:  method,
		Params:  rawParams,
	}
	if err := s.writeMessage(msg); err != nil {
		return rpcMessage{}, err
	}

	select {
	case resp := <-ch:
		return resp, nil
	case <-ctx.Done():
		return rpcMessage{}, ctx.Err()
	}
}

func (s *Server) deliverResponse(msg rpcMessage) {
	if msg.ID == nil {
		return
	}
	key := strings.TrimSpace(string(*msg.ID))
	s.pendingMu.Lock()
	ch, ok := s.pending[key]
	s.pendingMu.Unlock()
	if ok {
		select {
		case ch <- msg:
		default:
		}
	}
}

func (s *Server) resolvePersistDir(args map[string]any) string {
	if wsPath := stringArg(args, "workspace_path"); wsPath != "" {
		expanded := chroma.ExpandPath(wsPath)
		if dir, ok := chroma.FindProjectPersistDir(expanded); ok {
			return dir
		}
		return filepath.Join(expanded, "chroma_data")
	}

	s.rootsMu.RLock()
	roots := append([]string(nil), s.roots...)
	s.rootsMu.RUnlock()

	for _, r := range roots {
		if dir, ok := chroma.FindProjectPersistDir(r); ok {
			return dir
		}
	}
	var primaryRoot string
	if len(roots) > 0 {
		primaryRoot = roots[0]
	}
	return chroma.ResolveMCPPersistDir(primaryRoot)
}

func (s *Server) callTool(ctx context.Context, name string, args map[string]any) (any, error) {
	persistDir := s.resolvePersistDir(args)
	store, err := s.manager.Get(persistDir)
	if err != nil {
		return nil, err
	}
	colName := stringArg(args, "collection_name")

	switch name {
	case "save_memory":
		content := stringArg(args, "content")
		if content == "" {
			return nil, fmt.Errorf("content is required")
		}
		meta := map[string]string{}
		if tags := stringSliceArg(args, "tags"); len(tags) > 0 {
			meta["tags"] = strings.Join(tags, ",")
		}
		if nodeType := stringArg(args, "node_type"); nodeType != "" {
			meta["node_type"] = nodeType
		}
		if links := stringSliceArg(args, "links"); len(links) > 0 {
			meta["links"] = strings.Join(links, ",")
		}
		docID, err := store.Save(ctx, colName, stringArg(args, "memory_id"), content, meta)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"id":          docID,
			"status":      "saved",
			"persist_dir": store.PersistDir,
		}, nil

	case "search_memory":
		query := stringArg(args, "query")
		if query == "" {
			return nil, fmt.Errorf("query is required")
		}
		nResults := intArg(args, "n_results", 5)
		records, err := store.Query(ctx, colName, query, nResults)
		if err != nil {
			return nil, err
		}
		results := make([]map[string]any, 0, len(records))
		for _, r := range records {
			item := map[string]any{
				"id":       r.ID,
				"content":  r.Content,
				"metadata": r.Metadata,
			}
			if r.Distance != nil {
				item["distance"] = *r.Distance
			}
			results = append(results, item)
		}
		return map[string]any{
			"results":     results,
			"persist_dir": store.PersistDir,
		}, nil

	case "delete_memory":
		memoryID := stringArg(args, "memory_id")
		if memoryID == "" {
			return nil, fmt.Errorf("memory_id is required")
		}
		if err := store.Delete(ctx, colName, memoryID); err != nil {
			return nil, err
		}
		return map[string]any{
			"id":          memoryID,
			"status":      "deleted",
			"persist_dir": store.PersistDir,
		}, nil

	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}

func (s *Server) sendResult(id *json.RawMessage, result any) {
	raw, _ := json.Marshal(result)
	_ = s.writeMessage(rpcMessage{
		JSONRPC: "2.0",
		ID:      id,
		Result:  raw,
	})
}

func (s *Server) sendError(id *json.RawMessage, code int, message string) {
	_ = s.writeMessage(rpcMessage{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &rpcError{Code: code, Message: message},
	})
}

func (s *Server) writeMessage(msg rpcMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, err = fmt.Fprintf(s.out, "%s\n", data)
	return err
}

func fileURIToPath(rawURI string) string {
	u, err := url.Parse(strings.TrimSpace(rawURI))
	if err != nil {
		return ""
	}
	if u.Scheme == "file" {
		return filepath.Clean(u.Path)
	}
	if u.Scheme == "" && strings.HasPrefix(rawURI, "/") {
		return filepath.Clean(rawURI)
	}
	return ""
}

func stringArg(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	v, ok := args[key]
	if !ok || v == nil {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

func intArg(args map[string]any, key string, fallback int) int {
	if args == nil {
		return fallback
	}
	v, ok := args[key]
	if !ok || v == nil {
		return fallback
	}
	switch n := v.(type) {
	case float64:
		if int(n) > 0 {
			return int(n)
		}
	case int:
		if n > 0 {
			return n
		}
	}
	return fallback
}

func stringSliceArg(args map[string]any, key string) []string {
	if args == nil {
		return nil
	}
	raw, ok := args[key]
	if !ok || raw == nil {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		if s, ok := it.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out
}

func toolDefinitions() []map[string]any {
	return []map[string]any{
		{
			"name":        "save_memory",
			"description": "Save or update an AI memory/knowledge node in ChromaDB.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"content": map[string]any{
						"type":        "string",
						"description": "Concise memory content to store.",
					},
					"memory_id": map[string]any{
						"type":        "string",
						"description": "Optional existing memory ID to update in-place.",
					},
					"tags": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Optional lowercase category tags (primary category first).",
					},
					"node_type": map[string]any{
						"type":        "string",
						"description": "Optional knowledge node type (e.g. preference, architecture, bugfix, checkpoint).",
					},
					"links": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Optional related memory IDs.",
					},
					"collection_name": map[string]any{
						"type":        "string",
						"description": "Optional ChromaDB collection name (defaults to default).",
					},
					"workspace_path": map[string]any{
						"type":        "string",
						"description": "Optional absolute workspace/project path override.",
					},
				},
				"required": []string{"content"},
			},
		},
		{
			"name":        "search_memory",
			"description": "Search stored AI memories in ChromaDB by semantic similarity.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Semantic search query.",
					},
					"n_results": map[string]any{
						"type":        "integer",
						"description": "Maximum number of results to return (default 5).",
						"default":     5,
					},
					"collection_name": map[string]any{
						"type":        "string",
						"description": "Optional ChromaDB collection name.",
					},
					"workspace_path": map[string]any{
						"type":        "string",
						"description": "Optional absolute workspace/project path override.",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			"name":        "delete_memory",
			"description": "Delete a stored AI memory from ChromaDB by its ID.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"memory_id": map[string]any{
						"type":        "string",
						"description": "Memory ID to delete.",
					},
					"collection_name": map[string]any{
						"type":        "string",
						"description": "Optional ChromaDB collection name.",
					},
					"workspace_path": map[string]any{
						"type":        "string",
						"description": "Optional absolute workspace/project path override.",
					},
				},
				"required": []string{"memory_id"},
			},
		},
	}
}

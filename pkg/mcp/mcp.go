// Package mcp is a minimal server of the Model Context Protocol: JSON-RPC 2.0
// over newline-delimited stdio, with the lifecycle (initialize, ping) and the
// tools capability (tools/list, tools/call). It is what an AI agent's host
// speaks to discover and call tools; physis uses it to put its API in front
// of agents (cmd/physis-mcp) without a dependency on an SDK.
//
// Tool calls are served concurrently, so a long call (a plan waiting for the
// scheduler) does not block a short one. Responses are written whole, one
// per line.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"
)

// ProtocolVersions are the MCP revisions this server speaks, newest first.
var ProtocolVersions = []string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}

// JSON-RPC error codes.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
)

// Tool is one callable tool. Handler receives the call's arguments as raw
// JSON; what it returns is sent back as the tool's structured result, and an
// error it returns is reported to the model as a failed call (isError), not
// as a protocol error, so the agent can read it and correct itself.
type Tool struct {
	Name        string
	Title       string
	Description string
	// InputSchema is a JSON Schema object.
	InputSchema json.RawMessage
	Handler     func(ctx context.Context, args json.RawMessage) (any, error)
}

// Server serves a fixed set of tools.
type Server struct {
	Name, Version string
	// Instructions tell the client's model how to use the server.
	Instructions string
	Tools        []Tool
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Serve reads requests from in and writes responses to out until in ends or
// ctx is cancelled. It waits for calls in flight before returning.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var mu sync.Mutex
	var wg sync.WaitGroup
	defer wg.Wait()
	write := func(r response) {
		r.JSONRPC = "2.0"
		if r.ID == nil {
			r.ID = json.RawMessage("null")
		}
		b, err := json.Marshal(r)
		if err != nil {
			b, _ = json.Marshal(response{JSONRPC: "2.0", ID: r.ID, Error: &rpcError{CodeInternalError, err.Error()}})
		}
		mu.Lock()
		defer mu.Unlock()
		out.Write(append(b, '\n'))
	}
	br := bufio.NewReaderSize(in, 1<<20)
	for {
		line, err := br.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			s.dispatch(ctx, bytes.TrimSpace(line), write, &wg)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
}

func (s *Server) dispatch(ctx context.Context, line []byte, write func(response), wg *sync.WaitGroup) {
	if line[0] == '[' {
		write(response{Error: &rpcError{CodeInvalidRequest, "batches are not supported"}})
		return
	}
	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		write(response{Error: &rpcError{CodeParseError, "parse error: " + err.Error()}})
		return
	}
	// A message without an id is a notification: it gets no answer. The
	// only ones a client sends here (initialized, cancelled) need no action.
	if req.ID == nil {
		return
	}
	if req.JSONRPC != "2.0" || req.Method == "" {
		write(response{ID: req.ID, Error: &rpcError{CodeInvalidRequest, "not a JSON-RPC 2.0 request"}})
		return
	}
	switch req.Method {
	case "initialize":
		write(response{ID: req.ID, Result: s.initialize(req.Params)})
	case "ping":
		write(response{ID: req.ID, Result: struct{}{}})
	case "tools/list":
		write(response{ID: req.ID, Result: s.list()})
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.Name == "" {
			write(response{ID: req.ID, Error: &rpcError{CodeInvalidParams, "tools/call needs a tool name"}})
			return
		}
		i := slices.IndexFunc(s.Tools, func(t Tool) bool { return t.Name == p.Name })
		if i < 0 {
			write(response{ID: req.ID, Error: &rpcError{CodeInvalidParams, fmt.Sprintf("unknown tool %q", p.Name)}})
			return
		}
		if len(p.Arguments) == 0 || string(p.Arguments) == "null" {
			p.Arguments = json.RawMessage("{}")
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			write(response{ID: req.ID, Result: call(ctx, s.Tools[i], p.Arguments)})
		}()
	default:
		write(response{ID: req.ID, Error: &rpcError{CodeMethodNotFound, "method not found: " + req.Method}})
	}
}

func (s *Server) initialize(params json.RawMessage) any {
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	json.Unmarshal(params, &p)
	v := ProtocolVersions[0]
	if slices.Contains(ProtocolVersions, p.ProtocolVersion) {
		v = p.ProtocolVersion
	}
	return map[string]any{
		"protocolVersion": v,
		"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
		"serverInfo":      map[string]any{"name": s.Name, "version": s.Version},
		"instructions":    s.Instructions,
	}
}

func (s *Server) list() any {
	type tool struct {
		Name        string          `json:"name"`
		Title       string          `json:"title,omitempty"`
		Description string          `json:"description"`
		InputSchema json.RawMessage `json:"inputSchema"`
	}
	out := make([]tool, 0, len(s.Tools))
	for _, t := range s.Tools {
		out = append(out, tool{t.Name, t.Title, t.Description, t.InputSchema})
	}
	return map[string]any{"tools": out}
}

// CallResult is the result of tools/call.
type CallResult struct {
	Content           []Content       `json:"content"`
	StructuredContent json.RawMessage `json:"structuredContent,omitempty"`
	IsError           bool            `json:"isError,omitempty"`
}

// Content is one content block; only text is used.
type Content struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func call(ctx context.Context, t Tool, args json.RawMessage) (res CallResult) {
	defer func() {
		if r := recover(); r != nil {
			res = CallResult{Content: []Content{{"text", fmt.Sprintf("tool %s failed: %v", t.Name, r)}}, IsError: true}
		}
	}()
	v, err := t.Handler(ctx, args)
	if err != nil {
		return CallResult{Content: []Content{{"text", err.Error()}}, IsError: true}
	}
	b, err := json.Marshal(v)
	if err != nil {
		return CallResult{Content: []Content{{"text", err.Error()}}, IsError: true}
	}
	res = CallResult{Content: []Content{{"text", string(b)}}}
	// Structured content must be a JSON object; the same JSON goes in the
	// text block for clients that read only text.
	if len(b) > 0 && b[0] == '{' {
		res.StructuredContent = b
	}
	return res
}

// DecodeArgs decodes tool arguments strictly: an unknown field is an error,
// so a misspelt constraint is refused instead of silently dropped.
func DecodeArgs(args json.RawMessage, v any) error {
	dec := json.NewDecoder(bytes.NewReader(args))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("invalid arguments: %w", err)
	}
	return nil
}

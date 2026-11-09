package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

func testServer() *Server {
	type echoArgs struct {
		Say string `json:"say"`
	}
	return &Server{Name: "test", Version: "0", Tools: []Tool{
		{Name: "echo", Description: "echoes", InputSchema: json.RawMessage(`{"type":"object","properties":{"say":{"type":"string"}},"additionalProperties":false}`),
			Handler: func(_ context.Context, args json.RawMessage) (any, error) {
				var a echoArgs
				if err := DecodeArgs(args, &a); err != nil {
					return nil, err
				}
				return map[string]string{"said": a.Say}, nil
			}},
		{Name: "fail", Description: "fails", InputSchema: json.RawMessage(`{"type":"object"}`),
			Handler: func(context.Context, json.RawMessage) (any, error) { return nil, errors.New("no such thing") }},
	}}
}

// run sends lines to a server and returns its responses by id.
func run(t *testing.T, lines ...string) map[string]map[string]any {
	t.Helper()
	var out strings.Builder
	if err := testServer().Serve(context.Background(), strings.NewReader(strings.Join(lines, "\n")+"\n"), &out); err != nil {
		t.Fatal(err)
	}
	got := map[string]map[string]any{}
	sc := bufio.NewScanner(strings.NewReader(out.String()))
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("response is not JSON: %q", sc.Text())
		}
		if m["jsonrpc"] != "2.0" {
			t.Fatalf("not JSON-RPC 2.0: %v", m)
		}
		id, _ := json.Marshal(m["id"])
		got[string(id)] = m
	}
	return got
}

func TestLifecycleAndToolCalls(t *testing.T) {
	got := run(t,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"c","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"echo","arguments":{"say":"hi"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"fail","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"echo","arguments":{"shout":"hi"}}}`,
		`{"jsonrpc":"2.0","id":"six","method":"ping"}`,
	)
	if len(got) != 6 {
		t.Fatalf("%d responses, want 6 (a notification gets none): %v", len(got), got)
	}
	init := got["1"]["result"].(map[string]any)
	if init["protocolVersion"] != "2025-06-18" || init["capabilities"].(map[string]any)["tools"] == nil {
		t.Fatalf("initialize: %v", init)
	}
	tools := got["2"]["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 2 || tools[0].(map[string]any)["inputSchema"].(map[string]any)["type"] != "object" {
		t.Fatalf("tools/list: %v", tools)
	}
	ok := got["3"]["result"].(map[string]any)
	if ok["isError"] == true || ok["structuredContent"].(map[string]any)["said"] != "hi" ||
		!strings.Contains(ok["content"].([]any)[0].(map[string]any)["text"].(string), `"said":"hi"`) {
		t.Fatalf("echo: %v", ok)
	}
	failed := got["4"]["result"].(map[string]any)
	if failed["isError"] != true || failed["content"].([]any)[0].(map[string]any)["text"] != "no such thing" {
		t.Fatalf("a failing tool must be a result with isError: %v", got["4"])
	}
	strict := got["5"]["result"].(map[string]any)
	if strict["isError"] != true || !strings.Contains(strict["content"].([]any)[0].(map[string]any)["text"].(string), "shout") {
		t.Fatalf("an unknown argument was accepted: %v", strict)
	}
	if _, ok := got[`"six"`]["result"]; !ok {
		t.Fatalf("ping: %v", got[`"six"`])
	}
}

func TestProtocolErrors(t *testing.T) {
	got := run(t,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"1999-01-01"}}`,
		`{"jsonrpc":"2.0","id":2,"method":"resources/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"nope"}}`,
		`{not json`,
	)
	if v := got["1"]["result"].(map[string]any)["protocolVersion"]; v != ProtocolVersions[0] {
		t.Fatalf("an unknown version must get the newest supported one, got %v", v)
	}
	code := func(id string) float64 { return got[id]["error"].(map[string]any)["code"].(float64) }
	if code("2") != CodeMethodNotFound || code("3") != CodeInvalidParams || code("null") != CodeParseError {
		t.Fatalf("error codes: %v", got)
	}
}

// Over a real pipe the server answers each request as it arrives, before
// the input ends: a host keeps the pipe open for the whole session.
func TestServesOverAnOpenPipe(t *testing.T) {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	done := make(chan error)
	go func() { done <- testServer().Serve(context.Background(), inR, outW); outW.Close() }()
	rd := bufio.NewReader(outR)
	for i, req := range []string{
		`{"jsonrpc":"2.0","id":1,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"echo","arguments":{"say":"x"}}}`,
	} {
		if _, err := io.WriteString(inW, req+"\n"); err != nil {
			t.Fatal(err)
		}
		line, err := rd.ReadBytes('\n')
		if err != nil {
			t.Fatal(err)
		}
		var m struct {
			ID float64 `json:"id"`
		}
		if json.Unmarshal(line, &m); int(m.ID) != i+1 {
			t.Fatalf("response %s to request %d", line, i+1)
		}
	}
	inW.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

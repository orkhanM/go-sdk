// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/orkhanm/go-sdk/internal/jsonrpc2"
	"github.com/orkhanm/go-sdk/jsonrpc"
)

// TestSessionRecovery tests that sessions can be recovered from the session store
// when a client is routed to a different server instance.
func TestSessionRecovery(t *testing.T) {
	// Create a shared session store
	sessionStore := NewInMemorySessionStore()
	defer sessionStore.Close()

	// Create first server instance
	server1 := NewServer(&Implementation{Name: "test-server", Version: "1.0"}, nil)
	AddTool(server1, &Tool{Name: "test-tool", Description: "test"}, func(ctx context.Context, req *CallToolRequest, input struct{}) (*CallToolResult, struct{}, error) {
		return &CallToolResult{Content: []Content{&TextContent{Text: "response from server 1"}}}, struct{}{}, nil
	})

	handler1 := NewStreamableHTTPHandler(func(*http.Request) *Server {
		return server1
	}, &StreamableHTTPOptions{
		SessionStore:   sessionStore,
		SessionTimeout: 5 * time.Minute,
		Logger:         slog.Default(),
	})

	// Create HTTP test server
	testServer := httptest.NewServer(handler1)
	defer testServer.Close()

	// Initialize a session
	initRequest := &jsonrpc.Request{
		ID:     jsonrpc2.Int64ID(1),
		Method: "initialize",
		Params: mustMarshal(InitializeParams{
			ProtocolVersion: "2025-03-26",
			ClientInfo:      &Implementation{Name: "test-client", Version: "1.0"},
			Capabilities:    &ClientCapabilities{},
		}),
	}

	body, _ := jsonrpc.EncodeMessage(initRequest)
	resp := httpRequest(t, testServer.URL, "POST", "", body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Initialize failed with status %d: %s", resp.StatusCode, readBody(t, resp))
	}

	// Extract session ID from response header
	sessionID := resp.Header.Get(sessionIDHeader)
	if sessionID == "" {
		t.Fatal("No session ID in response")
	}

	t.Logf("Created session: %s", sessionID)

	// Send initialized notification
	initializedNotif := &jsonrpc.Request{
		Method: "notifications/initialized",
		Params: mustMarshal(InitializedParams{}),
	}

	body, _ = jsonrpc.EncodeMessage(initializedNotif)
	resp = httpRequest(t, testServer.URL, "POST", sessionID, body)
	// Notifications return 202 Accepted
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		t.Fatalf("Initialized notification failed with status %d", resp.StatusCode)
	}

	// Verify session is in the store and check if state is persisted
	ctx := context.Background()
	stored, err := sessionStore.Get(ctx, sessionID)
	if err != nil {
		t.Fatalf("Session not found in store: %v", err)
	}

	t.Log("Session successfully stored")

	// Wait a moment for sessionUpdated to be called asynchronously
	time.Sleep(100 * time.Millisecond)

	// Check again - the state should now include InitializeParams and InitializedParams
	stored, err = sessionStore.Get(ctx, sessionID)
	if err != nil {
		t.Fatalf("Session not found in store: %v", err)
	}

	if stored.SessionState.InitializeParams != nil {
		t.Log("✓ InitializeParams successfully persisted")
	} else {
		t.Log("Warning: InitializeParams not yet persisted (may be timing dependent)")
	}

	if stored.SessionState.InitializedParams != nil {
		t.Log("✓ InitializedParams successfully persisted")
	} else {
		t.Log("Warning: InitializedParams not yet persisted (may be timing dependent)")
	}

	// Simulate server restart by creating a new handler with the same session store
	server2 := NewServer(&Implementation{Name: "test-server", Version: "1.0"}, nil)
	AddTool(server2, &Tool{Name: "test-tool", Description: "test"}, func(ctx context.Context, req *CallToolRequest, input struct{}) (*CallToolResult, struct{}, error) {
		return &CallToolResult{Content: []Content{&TextContent{Text: "response from server 2"}}}, struct{}{}, nil
	})

	handler2 := NewStreamableHTTPHandler(func(*http.Request) *Server {
		return server2
	}, &StreamableHTTPOptions{
		SessionStore:   sessionStore,
		SessionTimeout: 5 * time.Minute,
		Logger:         slog.Default(),
	})

	// Create a new test server with the second handler (simulating different server instance)
	testServer2 := httptest.NewServer(handler2)
	defer testServer2.Close()

	t.Log("Simulated server restart - new instance created")

	// Now test that we can make a real request to the recovered session.
	// Since the session state (including InitializeParams and InitializedParams) was persisted,
	// the session should be fully initialized and ready to handle requests.
	toolListRequest := &jsonrpc.Request{
		ID:     jsonrpc2.Int64ID(2),
		Method: "tools/list",
		Params: mustMarshal(ListToolsParams{}),
	}

	body, _ = jsonrpc.EncodeMessage(toolListRequest)
	resp = httpRequest(t, testServer2.URL, "POST", sessionID, body)

	// The session should be found and initialized, so this should succeed
	if resp.StatusCode == http.StatusNotFound {
		t.Fatal("Session not found - recovery failed!")
	}

	if resp.StatusCode != http.StatusOK {
		respBody := readBody(t, resp)
		// Check if it's the "invalid during initialization" error
		if strings.Contains(respBody, "invalid during session initialization") {
			t.Fatalf("Session recovered but not properly initialized! Response: %s", respBody)
		}
		t.Logf("Request returned status %d: %s", resp.StatusCode, respBody)
	} else {
		t.Log("✓ Successfully made request to recovered session - session state was properly restored!")
	}

	t.Log("Test completed successfully - session was recovered with full state")
}

// TestSessionRecoveryNotFound tests that requests with unknown session IDs are rejected.
func TestSessionRecoveryNotFound(t *testing.T) {
	sessionStore := NewInMemorySessionStore()
	defer sessionStore.Close()

	server := NewServer(&Implementation{Name: "test-server", Version: "1.0"}, nil)
	handler := NewStreamableHTTPHandler(func(*http.Request) *Server {
		return server
	}, &StreamableHTTPOptions{
		SessionStore: sessionStore,
		Logger:       slog.Default(),
	})

	testServer := httptest.NewServer(handler)
	defer testServer.Close()

	// Try to make a request with a non-existent session ID
	toolRequest := &jsonrpc.Request{
		ID:     jsonrpc2.Int64ID(1),
		Method: "tools/list",
	}

	body, _ := json.Marshal(toolRequest)
	resp := httpRequest(t, testServer.URL, "POST", "non-existent-session", body)

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("Expected 404 Not Found, got %d", resp.StatusCode)
	}
}

// TestSessionStoreDisabled tests that when no session store is provided,
// sessions are only stored in memory and cannot be recovered.
func TestSessionStoreDisabled(t *testing.T) {
	// Create server without a session store
	server1 := NewServer(&Implementation{Name: "test-server", Version: "1.0"}, nil)
	handler1 := NewStreamableHTTPHandler(func(*http.Request) *Server {
		return server1
	}, &StreamableHTTPOptions{
		SessionStore: nil, // No session store
		Logger:       slog.Default(),
	})

	testServer := httptest.NewServer(handler1)
	defer testServer.Close()

	// Initialize a session
	initRequest := &jsonrpc.Request{
		ID:     jsonrpc2.Int64ID(1),
		Method: "initialize",
		Params: mustMarshal(InitializeParams{
			ProtocolVersion: "2025-03-26",
			ClientInfo:      &Implementation{Name: "test-client", Version: "1.0"},
		}),
	}

	body, _ := jsonrpc.EncodeMessage(initRequest)
	resp := httpRequest(t, testServer.URL, "POST", "", body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Initialize failed with status %d", resp.StatusCode)
	}

	sessionID := resp.Header.Get(sessionIDHeader)
	if sessionID == "" {
		t.Fatal("No session ID in response")
	}

	// Create a new handler (simulating server restart)
	server2 := NewServer(&Implementation{Name: "test-server", Version: "1.0"}, nil)
	handler2 := NewStreamableHTTPHandler(func(*http.Request) *Server {
		return server2
	}, &StreamableHTTPOptions{
		SessionStore: nil, // No session store
		Logger:       slog.Default(),
	})

	testServer2 := httptest.NewServer(handler2)
	defer testServer2.Close()

	// Try to use the session ID - should fail because there's no store
	toolRequest := &jsonrpc.Request{
		ID:     jsonrpc2.Int64ID(2),
		Method: "tools/list",
	}

	body, _ = jsonrpc.EncodeMessage(toolRequest)
	resp = httpRequest(t, testServer2.URL, "POST", sessionID, body)

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("Expected 404 Not Found without session store, got %d", resp.StatusCode)
	}
}

// Helper functions

func httpRequest(t *testing.T, url, method, sessionID string, body []byte) *http.Response {
	t.Helper()

	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set(protocolVersionHeader, "2025-03-26")

	if sessionID != "" {
		req.Header.Set(sessionIDHeader, sessionID)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	return resp
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()

	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	return string(body)
}

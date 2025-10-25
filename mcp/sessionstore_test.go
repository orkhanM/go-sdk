// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package mcp

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestInMemorySessionStore_BasicOperations(t *testing.T) {
	store := NewInMemorySessionStore()
	defer store.Close()

	ctx := context.Background()
	sessionID := "test-session-1"

	// Test Put
	info := &StoredSessionInfo{
		SessionState: ServerSessionState{
			InitializeParams: &InitializeParams{
				ProtocolVersion: "2025-03-26",
			},
			InitializedParams: &InitializedParams{},
			LogLevel:          "info",
		},
		Refs:           0,
		Timeout:        5 * time.Minute,
		CreatedAt:      time.Now(),
		LastAccessedAt: time.Now(),
	}

	err := store.Put(ctx, sessionID, info, 10*time.Minute)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Test Get
	retrieved, err := store.Get(ctx, sessionID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if retrieved.SessionState.LogLevel != info.SessionState.LogLevel {
		t.Errorf("LogLevel mismatch: got %s, want %s", retrieved.SessionState.LogLevel, info.SessionState.LogLevel)
	}

	if retrieved.Timeout != info.Timeout {
		t.Errorf("Timeout mismatch: got %v, want %v", retrieved.Timeout, info.Timeout)
	}

	// Test Get non-existent session
	_, err = store.Get(ctx, "non-existent")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("Expected ErrSessionNotFound, got %v", err)
	}

	// Test Delete
	err = store.Delete(ctx, sessionID)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify deletion
	_, err = store.Get(ctx, sessionID)
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("Expected ErrSessionNotFound after delete, got %v", err)
	}

	// Test Delete non-existent session (should not error)
	err = store.Delete(ctx, "non-existent")
	if err != nil {
		t.Errorf("Delete of non-existent session should not error, got %v", err)
	}
}

func TestInMemorySessionStore_UpdateRefs(t *testing.T) {
	store := NewInMemorySessionStore()
	defer store.Close()

	ctx := context.Background()
	sessionID := "test-session-refs"

	// Put initial session
	info := &StoredSessionInfo{
		SessionState:   ServerSessionState{LogLevel: "info"},
		Refs:           0,
		CreatedAt:      time.Now(),
		LastAccessedAt: time.Now(),
	}

	err := store.Put(ctx, sessionID, info, 10*time.Minute)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Increment refs
	newRefs, err := store.UpdateRefs(ctx, sessionID, 1)
	if err != nil {
		t.Fatalf("UpdateRefs failed: %v", err)
	}
	if newRefs != 1 {
		t.Errorf("Expected refs=1, got %d", newRefs)
	}

	// Increment again
	newRefs, err = store.UpdateRefs(ctx, sessionID, 1)
	if err != nil {
		t.Fatalf("UpdateRefs failed: %v", err)
	}
	if newRefs != 2 {
		t.Errorf("Expected refs=2, got %d", newRefs)
	}

	// Decrement
	newRefs, err = store.UpdateRefs(ctx, sessionID, -1)
	if err != nil {
		t.Fatalf("UpdateRefs failed: %v", err)
	}
	if newRefs != 1 {
		t.Errorf("Expected refs=1, got %d", newRefs)
	}

	// Verify value persisted
	retrieved, err := store.Get(ctx, sessionID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if retrieved.Refs != 1 {
		t.Errorf("Expected persisted refs=1, got %d", retrieved.Refs)
	}

	// Test UpdateRefs on non-existent session
	_, err = store.UpdateRefs(ctx, "non-existent", 1)
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("Expected ErrSessionNotFound, got %v", err)
	}
}

func TestInMemorySessionStore_RefreshTTL(t *testing.T) {
	store := NewInMemorySessionStore()
	defer store.Close()

	ctx := context.Background()
	sessionID := "test-session-ttl"

	// Put session with short TTL
	info := &StoredSessionInfo{
		SessionState:   ServerSessionState{LogLevel: "info"},
		CreatedAt:      time.Now(),
		LastAccessedAt: time.Now(),
	}

	err := store.Put(ctx, sessionID, info, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Wait a bit but not long enough to expire
	time.Sleep(50 * time.Millisecond)

	// Refresh the TTL
	err = store.RefreshTTL(ctx, sessionID, 10*time.Minute)
	if err != nil {
		t.Fatalf("RefreshTTL failed: %v", err)
	}

	// Wait past the original TTL
	time.Sleep(100 * time.Millisecond)

	// Session should still exist due to refreshed TTL
	_, err = store.Get(ctx, sessionID)
	if err != nil {
		t.Errorf("Session should exist after TTL refresh, got error: %v", err)
	}

	// Test RefreshTTL on non-existent session
	err = store.RefreshTTL(ctx, "non-existent", 1*time.Minute)
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("Expected ErrSessionNotFound, got %v", err)
	}
}

func TestInMemorySessionStore_Expiration(t *testing.T) {
	store := NewInMemorySessionStore()
	defer store.Close()

	ctx := context.Background()
	sessionID := "test-session-expire"

	// Put session with very short TTL
	info := &StoredSessionInfo{
		SessionState:   ServerSessionState{LogLevel: "info"},
		CreatedAt:      time.Now(),
		LastAccessedAt: time.Now(),
	}

	err := store.Put(ctx, sessionID, info, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Verify it exists initially
	_, err = store.Get(ctx, sessionID)
	if err != nil {
		t.Fatalf("Session should exist initially, got error: %v", err)
	}

	// Wait for expiration
	time.Sleep(100 * time.Millisecond)

	// Session should be expired
	_, err = store.Get(ctx, sessionID)
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("Expected ErrSessionNotFound for expired session, got %v", err)
	}
}

func TestInMemorySessionStore_NoExpiration(t *testing.T) {
	store := NewInMemorySessionStore()
	defer store.Close()

	ctx := context.Background()
	sessionID := "test-session-no-expire"

	// Put session with zero TTL (no expiration)
	info := &StoredSessionInfo{
		SessionState:   ServerSessionState{LogLevel: "info"},
		CreatedAt:      time.Now(),
		LastAccessedAt: time.Now(),
	}

	err := store.Put(ctx, sessionID, info, 0)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Wait a bit
	time.Sleep(100 * time.Millisecond)

	// Session should still exist
	_, err = store.Get(ctx, sessionID)
	if err != nil {
		t.Errorf("Session with no TTL should not expire, got error: %v", err)
	}
}

func TestInMemorySessionStore_Replace(t *testing.T) {
	store := NewInMemorySessionStore()
	defer store.Close()

	ctx := context.Background()
	sessionID := "test-session-replace"

	// Put initial session
	info1 := &StoredSessionInfo{
		SessionState:   ServerSessionState{LogLevel: "info"},
		Refs:           1,
		CreatedAt:      time.Now(),
		LastAccessedAt: time.Now(),
	}

	err := store.Put(ctx, sessionID, info1, 10*time.Minute)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Replace with new session
	info2 := &StoredSessionInfo{
		SessionState:   ServerSessionState{LogLevel: "debug"},
		Refs:           2,
		CreatedAt:      time.Now(),
		LastAccessedAt: time.Now(),
	}

	err = store.Put(ctx, sessionID, info2, 10*time.Minute)
	if err != nil {
		t.Fatalf("Put (replace) failed: %v", err)
	}

	// Verify replacement
	retrieved, err := store.Get(ctx, sessionID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if retrieved.SessionState.LogLevel != "debug" {
		t.Errorf("Expected LogLevel=debug, got %s", retrieved.SessionState.LogLevel)
	}

	if retrieved.Refs != 2 {
		t.Errorf("Expected Refs=2, got %d", retrieved.Refs)
	}
}

func TestInMemorySessionStore_Cleanup(t *testing.T) {
	store := NewInMemorySessionStore()
	defer store.Close()

	ctx := context.Background()

	// Add multiple sessions with short TTLs
	for i := 0; i < 5; i++ {
		sessionID := "test-session-cleanup-" + string(rune('a'+i))
		info := &StoredSessionInfo{
			SessionState:   ServerSessionState{LogLevel: "info"},
			CreatedAt:      time.Now(),
			LastAccessedAt: time.Now(),
		}
		err := store.Put(ctx, sessionID, info, 50*time.Millisecond)
		if err != nil {
			t.Fatalf("Put failed: %v", err)
		}
	}

	// Add one session with no expiration
	infoNoExpire := &StoredSessionInfo{
		SessionState:   ServerSessionState{LogLevel: "info"},
		CreatedAt:      time.Now(),
		LastAccessedAt: time.Now(),
	}
	err := store.Put(ctx, "test-session-permanent", infoNoExpire, 0)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Trigger cleanup by waiting and calling cleanup directly
	time.Sleep(100 * time.Millisecond)
	store.cleanup()

	// Verify expired sessions are gone
	for i := 0; i < 5; i++ {
		sessionID := "test-session-cleanup-" + string(rune('a'+i))
		_, err := store.Get(ctx, sessionID)
		if !errors.Is(err, ErrSessionNotFound) {
			t.Errorf("Session %s should have been cleaned up, got error: %v", sessionID, err)
		}
	}

	// Verify permanent session still exists
	_, err = store.Get(ctx, "test-session-permanent")
	if err != nil {
		t.Errorf("Permanent session should still exist, got error: %v", err)
	}
}

func TestInMemorySessionStore_ConcurrentAccess(t *testing.T) {
	store := NewInMemorySessionStore()
	defer store.Close()

	ctx := context.Background()
	sessionID := "test-session-concurrent"

	// Put initial session
	info := &StoredSessionInfo{
		SessionState:   ServerSessionState{LogLevel: "info"},
		Refs:           0,
		CreatedAt:      time.Now(),
		LastAccessedAt: time.Now(),
	}

	err := store.Put(ctx, sessionID, info, 10*time.Minute)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Concurrently increment and decrement refs
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				store.UpdateRefs(ctx, sessionID, 1)
				store.UpdateRefs(ctx, sessionID, -1)
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Final ref count should be 0
	retrieved, err := store.Get(ctx, sessionID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if retrieved.Refs != 0 {
		t.Errorf("Expected final refs=0, got %d", retrieved.Refs)
	}
}

func TestInMemorySessionStore_IsolatedCopies(t *testing.T) {
	store := NewInMemorySessionStore()
	defer store.Close()

	ctx := context.Background()
	sessionID := "test-session-isolation"

	// Put initial session
	info := &StoredSessionInfo{
		SessionState:   ServerSessionState{LogLevel: "info"},
		Refs:           5,
		CreatedAt:      time.Now(),
		LastAccessedAt: time.Now(),
	}

	err := store.Put(ctx, sessionID, info, 10*time.Minute)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Get the session
	retrieved, err := store.Get(ctx, sessionID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	// Modify the retrieved copy
	retrieved.Refs = 100
	retrieved.SessionState.LogLevel = "debug"

	// Get again and verify original values are unchanged
	retrieved2, err := store.Get(ctx, sessionID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if retrieved2.Refs != 5 {
		t.Errorf("Store should return copies - expected refs=5, got %d", retrieved2.Refs)
	}

	if retrieved2.SessionState.LogLevel != "info" {
		t.Errorf("Store should return copies - expected LogLevel=info, got %s", retrieved2.SessionState.LogLevel)
	}
}

// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package mcp

import (
	"context"
	"errors"
	"sync"
	"time"
)

var (
	// ErrSessionNotFound is returned when a session is not found in the store.
	ErrSessionNotFound = errors.New("session not found")
)

// SessionStore is an interface for storing and retrieving MCP sessions.
//
// This interface enables distributed MCP deployments where multiple server
// instances can share session state through a common storage backend (e.g., Redis,
// PostgreSQL, etcd). When clients connect to different server instances behind a
// load balancer, their sessions can be recovered from the shared store.
//
// The SessionStore is used exclusively by [StreamableHTTPHandler] to persist
// stateful HTTP sessions. It is not used for other transport types (stdio, SSE, etc.).
//
// Implementations must be safe for concurrent use by multiple goroutines.
//
// # TTL and Expiration
//
// The store is responsible for automatically cleaning up expired sessions based
// on the TTL (time-to-live) provided in the Put operation. When a session expires,
// it should be automatically removed from the store, and subsequent Get calls
// should return ErrSessionNotFound.
//
// # Reference Counting
//
// Sessions use reference counting to track concurrent HTTP POST requests.
// Implementations must preserve the refs field when storing and retrieving
// session data to ensure timeout logic works correctly across server instances.
//
// # Example Implementation
//
// See the examples/server/redis-sessions directory for a complete Redis-based
// implementation.
type SessionStore interface {
	// Get retrieves a session by its ID.
	//
	// Returns ErrSessionNotFound if the session does not exist or has expired.
	Get(ctx context.Context, sessionID string) (*StoredSessionInfo, error)

	// Put stores a session with the specified TTL (time-to-live).
	//
	// If ttl is zero or negative, the session should not expire automatically.
	// If a session with the same ID already exists, it should be replaced.
	//
	// The store is responsible for cleaning up expired sessions.
	Put(ctx context.Context, sessionID string, info *StoredSessionInfo, ttl time.Duration) error

	// Delete removes a session from the store.
	//
	// Returns ErrSessionNotFound if the session does not exist.
	// It is safe to call Delete on an already-deleted session.
	Delete(ctx context.Context, sessionID string) error

	// UpdateRefs atomically updates the reference count for a session.
	//
	// This is used to track concurrent HTTP POST requests. The delta can be
	// positive (increment) or negative (decrement).
	//
	// Returns the new reference count after applying the delta.
	// Returns ErrSessionNotFound if the session does not exist.
	//
	// Implementations must ensure this operation is atomic, even across
	// distributed deployments.
	UpdateRefs(ctx context.Context, sessionID string, delta int) (int, error)

	// RefreshTTL resets the TTL for a session to the specified duration.
	//
	// This is called when a session becomes idle (refs == 0) to restart
	// the inactivity timeout.
	//
	// Returns ErrSessionNotFound if the session does not exist.
	RefreshTTL(ctx context.Context, sessionID string, ttl time.Duration) error
}

// StoredSessionInfo contains the session data that can be persisted to external storage.
//
// This is a subset of the full sessionInfo structure, containing only the data
// that needs to be shared across server instances. Runtime state like timers
// and mutexes are not included.
type StoredSessionInfo struct {
	// SessionState is the MCP session state (initialization params, etc.)
	SessionState ServerSessionState `json:"sessionState"`

	// Refs is the reference count for concurrent POST requests.
	// When refs > 0, the session timeout is paused.
	Refs int `json:"refs"`

	// Timeout is the idle timeout duration for this session.
	// Zero means no timeout.
	Timeout time.Duration `json:"timeout"`

	// CreatedAt is when the session was first created.
	CreatedAt time.Time `json:"createdAt"`

	// LastAccessedAt is when the session was last accessed.
	LastAccessedAt time.Time `json:"lastAccessedAt"`
}

// InMemorySessionStore is a simple in-memory implementation of SessionStore.
//
// This is the default implementation used when no custom SessionStore is provided.
// It stores sessions in a map and uses background goroutines to clean up expired
// sessions.
//
// This implementation is suitable for single-instance deployments. For distributed
// deployments, use a shared storage backend like Redis.
type InMemorySessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*memorySessionEntry
	stopCh   chan struct{}
	once     sync.Once
}

type memorySessionEntry struct {
	info      *StoredSessionInfo
	expiresAt time.Time // zero time means no expiration
}

// NewInMemorySessionStore creates a new in-memory session store.
//
// The store will automatically clean up expired sessions in the background.
// Call Close() to stop the cleanup goroutine when the store is no longer needed.
func NewInMemorySessionStore() *InMemorySessionStore {
	s := &InMemorySessionStore{
		sessions: make(map[string]*memorySessionEntry),
		stopCh:   make(chan struct{}),
	}
	go s.cleanupLoop()
	return s
}

// Get implements SessionStore.Get.
func (s *InMemorySessionStore) Get(ctx context.Context, sessionID string) (*StoredSessionInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.sessions[sessionID]
	if !ok {
		return nil, ErrSessionNotFound
	}

	// Check if expired
	if !entry.expiresAt.IsZero() && time.Now().After(entry.expiresAt) {
		return nil, ErrSessionNotFound
	}

	// Return a copy to avoid external modifications
	infoCopy := *entry.info
	return &infoCopy, nil
}

// Put implements SessionStore.Put.
func (s *InMemorySessionStore) Put(ctx context.Context, sessionID string, info *StoredSessionInfo, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Make a copy to avoid external modifications
	infoCopy := *info
	entry := &memorySessionEntry{
		info: &infoCopy,
	}

	if ttl > 0 {
		entry.expiresAt = time.Now().Add(ttl)
	}

	s.sessions[sessionID] = entry
	return nil
}

// Delete implements SessionStore.Delete.
func (s *InMemorySessionStore) Delete(ctx context.Context, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.sessions, sessionID)
	return nil
}

// UpdateRefs implements SessionStore.UpdateRefs.
func (s *InMemorySessionStore) UpdateRefs(ctx context.Context, sessionID string, delta int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.sessions[sessionID]
	if !ok {
		return 0, ErrSessionNotFound
	}

	// Check if expired
	if !entry.expiresAt.IsZero() && time.Now().After(entry.expiresAt) {
		return 0, ErrSessionNotFound
	}

	entry.info.Refs += delta
	if entry.info.Refs < 0 {
		entry.info.Refs = 0 // safety guard
	}

	return entry.info.Refs, nil
}

// RefreshTTL implements SessionStore.RefreshTTL.
func (s *InMemorySessionStore) RefreshTTL(ctx context.Context, sessionID string, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.sessions[sessionID]
	if !ok {
		return ErrSessionNotFound
	}

	// Check if expired
	if !entry.expiresAt.IsZero() && time.Now().After(entry.expiresAt) {
		return ErrSessionNotFound
	}

	if ttl > 0 {
		entry.expiresAt = time.Now().Add(ttl)
	} else {
		entry.expiresAt = time.Time{} // no expiration
	}

	return nil
}

// cleanupLoop runs in the background and removes expired sessions.
func (s *InMemorySessionStore) cleanupLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.cleanup()
		case <-s.stopCh:
			return
		}
	}
}

// cleanup removes expired sessions.
func (s *InMemorySessionStore) cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for id, entry := range s.sessions {
		if !entry.expiresAt.IsZero() && now.After(entry.expiresAt) {
			delete(s.sessions, id)
		}
	}
}

// Close stops the background cleanup goroutine.
//
// After calling Close, the store should not be used.
func (s *InMemorySessionStore) Close() error {
	s.once.Do(func() {
		close(s.stopCh)
	})
	return nil
}

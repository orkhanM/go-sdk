# Redis Session Store Example

This example demonstrates how to implement a custom `SessionStore` using Redis for persistent session storage across distributed MCP server instances.

## Overview

When running multiple MCP server instances behind a load balancer, client sessions need to be shared across instances. The `SessionStore` interface allows you to implement persistent session storage using any backend (Redis, PostgreSQL, etcd, etc.).

## Implementation

Below is a reference implementation of a Redis-based session store:

```go
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/orkhanm/go-sdk/mcp"
	"github.com/redis/go-redis/v9"
)

type RedisSessionStore struct {
	client *redis.Client
	prefix string // key prefix to namespace sessions
}

func NewRedisSessionStore(client *redis.Client) *RedisSessionStore {
	return &RedisSessionStore{
		client: client,
		prefix: "mcp:session:",
	}
}

func (s *RedisSessionStore) key(sessionID string) string {
	return s.prefix + sessionID
}

func (s *RedisSessionStore) Get(ctx context.Context, sessionID string) (*mcp.StoredSessionInfo, error) {
	data, err := s.client.Get(ctx, s.key(sessionID)).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, mcp.ErrSessionNotFound
		}
		return nil, fmt.Errorf("redis get: %w", err)
	}

	var info mcp.StoredSessionInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("unmarshal session: %w", err)
	}

	return &info, nil
}

func (s *RedisSessionStore) Put(ctx context.Context, sessionID string, info *mcp.StoredSessionInfo, ttl time.Duration) error {
	data, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}

	if err := s.client.Set(ctx, s.key(sessionID), data, ttl).Err(); err != nil {
		return fmt.Errorf("redis set: %w", err)
	}

	return nil
}

func (s *RedisSessionStore) Delete(ctx context.Context, sessionID string) error {
	if err := s.client.Del(ctx, s.key(sessionID)).Err(); err != nil {
		return fmt.Errorf("redis del: %w", err)
	}
	return nil
}

func (s *RedisSessionStore) UpdateRefs(ctx context.Context, sessionID string, delta int) (int, error) {
	// Use a Lua script to atomically update refs and get the new value
	script := `
		local key = KEYS[1]
		local delta = tonumber(ARGV[1])
		local data = redis.call('GET', key)
		if not data then
			return {err = 'not_found'}
		end

		local info = cjson.decode(data)
		info.refs = (info.refs or 0) + delta
		if info.refs < 0 then
			info.refs = 0
		end

		redis.call('SET', key, cjson.encode(info), 'KEEPTTL')
		return info.refs
	`

	result, err := s.client.Eval(ctx, script, []string{s.key(sessionID)}, delta).Result()
	if err != nil {
		if err.Error() == "not_found" {
			return 0, mcp.ErrSessionNotFound
		}
		return 0, fmt.Errorf("redis eval: %w", err)
	}

	refs, ok := result.(int64)
	if !ok {
		return 0, fmt.Errorf("unexpected result type: %T", result)
	}

	return int(refs), nil
}

func (s *RedisSessionStore) RefreshTTL(ctx context.Context, sessionID string, ttl time.Duration) error {
	ok, err := s.client.Expire(ctx, s.key(sessionID), ttl).Result()
	if err != nil {
		return fmt.Errorf("redis expire: %w", err)
	}
	if !ok {
		return mcp.ErrSessionNotFound
	}
	return nil
}
```

## Usage

```go
package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"time"

	"github.com/orkhanm/go-sdk/mcp"
	"github.com/redis/go-redis/v9"
)

func main() {
	// Connect to Redis
	redisClient := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "", // no password
		DB:       0,  // use default DB
	})

	// Test connection
	ctx := context.Background()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}

	// Create session store
	sessionStore := NewRedisSessionStore(redisClient)

	// Create MCP server
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "distributed-server",
		Version: "1.0.0",
	}, nil)

	// Add your tools, prompts, resources here
	// mcp.AddTool(server, ...)

	// Create HTTP handler with Redis session store
	handler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		return server
	}, &mcp.StreamableHTTPOptions{
		SessionStore:   sessionStore,
		SessionTimeout: 30 * time.Minute,
		Logger:         slog.Default(),
	})

	// Start HTTP server
	log.Println("Starting MCP server on :8080")
	if err := http.ListenAndServe(":8080", handler); err != nil {
		log.Fatal(err)
	}
}
```

## Deployment

When deploying multiple instances:

1. **Shared Redis**: All server instances connect to the same Redis instance/cluster
2. **Load Balancer**: Configure your load balancer to distribute requests across instances
3. **Session Affinity**: NOT required - sessions are recovered from Redis automatically

Example with Docker Compose:

```yaml
version: '3.8'

services:
  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"
    volumes:
      - redis-data:/data

  mcp-server:
    build: .
    environment:
      - REDIS_ADDR=redis:6379
    depends_on:
      - redis
    deploy:
      replicas: 3  # Run 3 instances
    ports:
      - "8080-8082:8080"

  nginx:
    image: nginx:alpine
    ports:
      - "80:80"
    volumes:
      - ./nginx.conf:/etc/nginx/nginx.conf
    depends_on:
      - mcp-server

volumes:
  redis-data:
```

## Benefits

- **High Availability**: Sessions persist even if a server instance crashes
- **Horizontal Scaling**: Add more server instances without session loss
- **Load Balancing**: Clients can be routed to any instance
- **Persistence**: Sessions survive server restarts

## Advanced Features

### Session Monitoring

```go
// Get all session IDs
func (s *RedisSessionStore) ListSessions(ctx context.Context) ([]string, error) {
	keys, err := s.client.Keys(ctx, s.prefix+"*").Result()
	if err != nil {
		return nil, err
	}

	sessions := make([]string, len(keys))
	for i, key := range keys {
		sessions[i] = strings.TrimPrefix(key, s.prefix)
	}
	return sessions, nil
}
```

### Session Metrics

```go
// Track session metrics
func (s *RedisSessionStore) GetMetrics(ctx context.Context) (*Metrics, error) {
	count, err := s.client.DBSize(ctx).Result()
	if err != nil {
		return nil, err
	}

	return &Metrics{
		ActiveSessions: count,
		// Add more metrics as needed
	}, nil
}
```

### Redis Cluster Support

For production deployments, use Redis Cluster:

```go
redisClient := redis.NewClusterClient(&redis.ClusterOptions{
	Addrs: []string{
		"redis-node-1:6379",
		"redis-node-2:6379",
		"redis-node-3:6379",
	},
	Password: os.Getenv("REDIS_PASSWORD"),
})
```

## Alternative Backends

The same `SessionStore` interface can be implemented for:

- **PostgreSQL**: Strong consistency, relational queries
- **DynamoDB**: AWS-native, automatic scaling
- **etcd**: Kubernetes-native, watch support
- **Memcached**: Simple, fast caching
- **MongoDB**: Document storage, flexible schema

## Testing

Test your session store implementation:

```go
func TestRedisSessionStore(t *testing.T) {
	// Use miniredis for testing without a real Redis instance
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	store := NewRedisSessionStore(client)

	// Test CRUD operations
	ctx := context.Background()
	info := &mcp.StoredSessionInfo{
		SessionState: mcp.ServerSessionState{
			LogLevel: "info",
		},
		Refs:      0,
		Timeout:   5 * time.Minute,
		CreatedAt: time.Now(),
	}

	// Put
	err = store.Put(ctx, "test-session", info, 10*time.Minute)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Get
	retrieved, err := store.Get(ctx, "test-session")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if retrieved.SessionState.LogLevel != "info" {
		t.Errorf("Expected LogLevel=info, got %s", retrieved.SessionState.LogLevel)
	}

	// Delete
	err = store.Delete(ctx, "test-session")
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify deletion
	_, err = store.Get(ctx, "test-session")
	if err != mcp.ErrSessionNotFound {
		t.Errorf("Expected ErrSessionNotFound, got %v", err)
	}
}
```

## Dependencies

```bash
go get github.com/redis/go-redis/v9
go get github.com/alicebob/miniredis/v2  # for testing
```

## See Also

- [Redis documentation](https://redis.io/docs/)
- [go-redis client](https://github.com/redis/go-redis)
- [MCP Specification](https://modelcontextprotocol.io/)

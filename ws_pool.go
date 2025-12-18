package hyperliquid

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

// StreamType represents the type of WebSocket stream.
type StreamType string

const (
	StreamTypeCandles      StreamType = "candles"
	StreamTypeTrades       StreamType = "trades"
	StreamTypeBBO          StreamType = "bbo"
	StreamTypeL2Book       StreamType = "l2book"
	StreamTypeAssetContext StreamType = "asset_context"

	// DefaultMaxConnections is the Hyperliquid limit
	DefaultMaxConnections = 100
)

// PooledConnection represents a WebSocket connection that can hold multiple subscriptions.
type PooledConnection struct {
	ID                int
	Client            *WebsocketClient
	Connected         bool
	LastError         error
	SubscriptionCount int64
	mu                sync.RWMutex
}

// ConnectionPool manages a fixed pool of WebSocket connections with round-robin subscription assignment.
type ConnectionPool struct {
	baseURL        string
	connections    []*PooledConnection
	maxConnections int
	nextConnIndex  atomic.Int64
	mu             sync.RWMutex
	opts           []WsOpt
	ctx            context.Context
	cancel         context.CancelFunc
	initialized    bool

	// Callbacks
	onConnectionError func(connID int, err error)
	onConnectionReady func(connID int)
}

// PoolOpt is a functional option for configuring the ConnectionPool.
type PoolOpt func(*ConnectionPool)

// WithPoolMaxConnections sets the maximum number of connections (default: 100).
func WithPoolMaxConnections(max int) PoolOpt {
	return func(p *ConnectionPool) {
		if max > 0 && max <= DefaultMaxConnections {
			p.maxConnections = max
		}
	}
}

// WithPoolOnConnectionError sets the callback for connection errors.
func WithPoolOnConnectionError(cb func(connID int, err error)) PoolOpt {
	return func(p *ConnectionPool) {
		p.onConnectionError = cb
	}
}

// WithPoolOnConnectionReady sets the callback for when a connection is ready.
func WithPoolOnConnectionReady(cb func(connID int)) PoolOpt {
	return func(p *ConnectionPool) {
		p.onConnectionReady = cb
	}
}

// WithPoolWsOpts sets the WebSocket options to apply to each connection.
func WithPoolWsOpts(opts ...WsOpt) PoolOpt {
	return func(p *ConnectionPool) {
		p.opts = opts
	}
}

// NewConnectionPool creates a new connection pool.
func NewConnectionPool(baseURL string, opts ...PoolOpt) *ConnectionPool {
	ctx, cancel := context.WithCancel(context.Background())
	pool := &ConnectionPool{
		baseURL:        baseURL,
		maxConnections: DefaultMaxConnections,
		connections:    nil,
		ctx:            ctx,
		cancel:         cancel,
	}
	for _, opt := range opts {
		opt(pool)
	}
	return pool
}

// Initialize creates all connections upfront.
func (p *ConnectionPool) Initialize(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.initialized {
		return nil
	}

	log.Printf("[POOL] Initializing %d connections...", p.maxConnections)
	p.connections = make([]*PooledConnection, p.maxConnections)

	var wg sync.WaitGroup
	errors := make(chan error, p.maxConnections)
	sem := make(chan struct{}, 10) // Limit concurrent connection attempts

	for i := 0; i < p.maxConnections; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sem <- struct{}{}        // Acquire
			defer func() { <-sem }() // Release

			// Create WebSocket client with pool-level options
			wsOpts := append([]WsOpt{}, p.opts...)
			wsOpts = append(wsOpts,
				WsOptOnDisconnect(func(err error) {
					p.handleDisconnect(idx, err)
				}),
				WsOptOnReconnect(func() {
					p.handleReconnect(idx)
				}),
			)

			client := NewWebsocketClient(p.baseURL, wsOpts...)

			conn := &PooledConnection{
				ID:        idx,
				Client:    client,
				Connected: false,
			}

			// Connect with retry
			var lastErr error
			for attempt := 0; attempt < 3; attempt++ {
				if err := client.Connect(ctx); err != nil {
					lastErr = err
					time.Sleep(time.Duration(attempt+1) * 100 * time.Millisecond)
					continue
				}
				conn.Connected = true
				conn.LastError = nil
				break
			}

			if !conn.Connected {
				conn.LastError = lastErr
				if p.onConnectionError != nil {
					p.onConnectionError(idx, lastErr)
				}
				errors <- fmt.Errorf("connection %d failed: %w", idx, lastErr)
			} else if p.onConnectionReady != nil {
				p.onConnectionReady(idx)
			}

			p.connections[idx] = conn
		}(i)
	}

	wg.Wait()
	close(errors)

	// Count successful connections
	connectedCount := 0
	for _, conn := range p.connections {
		if conn != nil && conn.Connected {
			connectedCount++
		}
	}

	log.Printf("[POOL] Initialized %d/%d connections", connectedCount, p.maxConnections)
	p.initialized = true

	if connectedCount == 0 {
		return fmt.Errorf("failed to establish any connections")
	}

	return nil
}

// GetNextConnection returns the next connection using round-robin.
func (p *ConnectionPool) GetNextConnection() *PooledConnection {
	if !p.initialized || len(p.connections) == 0 {
		return nil
	}

	// Round-robin selection
	idx := p.nextConnIndex.Add(1) % int64(len(p.connections))
	conn := p.connections[idx]

	// Skip disconnected connections
	for attempts := 0; attempts < len(p.connections); attempts++ {
		if conn != nil && conn.Connected {
			atomic.AddInt64(&conn.SubscriptionCount, 1)
			return conn
		}
		idx = (idx + 1) % int64(len(p.connections))
		conn = p.connections[idx]
	}

	// All connections down, return first one anyway
	return p.connections[0]
}

// handleDisconnect handles a connection disconnect.
func (p *ConnectionPool) handleDisconnect(connID int, err error) {
	p.mu.RLock()
	if connID < len(p.connections) && p.connections[connID] != nil {
		conn := p.connections[connID]
		conn.mu.Lock()
		conn.Connected = false
		conn.LastError = err
		conn.mu.Unlock()
	}
	p.mu.RUnlock()

	if p.onConnectionError != nil {
		p.onConnectionError(connID, err)
	}
}

// handleReconnect handles a successful reconnection.
func (p *ConnectionPool) handleReconnect(connID int) {
	p.mu.RLock()
	if connID < len(p.connections) && p.connections[connID] != nil {
		conn := p.connections[connID]
		conn.mu.Lock()
		conn.Connected = true
		conn.LastError = nil
		conn.mu.Unlock()
	}
	p.mu.RUnlock()

	if p.onConnectionReady != nil {
		p.onConnectionReady(connID)
	}
}

// Close closes all connections in the pool.
func (p *ConnectionPool) Close() error {
	p.cancel()

	p.mu.Lock()
	defer p.mu.Unlock()

	var lastErr error
	for _, conn := range p.connections {
		if conn != nil && conn.Client != nil {
			if err := conn.Client.Close(); err != nil {
				lastErr = err
			}
		}
	}
	p.connections = nil
	p.initialized = false
	return lastErr
}

// Stats returns pool statistics.
func (p *ConnectionPool) Stats() PoolStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	stats := PoolStats{
		TotalConnections: len(p.connections),
	}

	for _, conn := range p.connections {
		if conn == nil {
			continue
		}
		conn.mu.RLock()
		if conn.Connected {
			stats.ConnectedCount++
		} else {
			stats.DisconnectedCount++
		}
		stats.TotalSubscriptions += conn.SubscriptionCount
		conn.mu.RUnlock()
	}

	return stats
}

// PoolStats contains pool statistics.
type PoolStats struct {
	TotalConnections   int
	ConnectedCount     int
	DisconnectedCount  int
	TotalSubscriptions int64
}

// HealthCheck returns the health status of all connections.
func (p *ConnectionPool) HealthCheck() []ConnectionHealth {
	p.mu.RLock()
	defer p.mu.RUnlock()

	health := make([]ConnectionHealth, 0, len(p.connections))
	for _, conn := range p.connections {
		if conn == nil {
			continue
		}
		conn.mu.RLock()
		h := ConnectionHealth{
			ConnID:            conn.ID,
			Connected:         conn.Connected,
			LastError:         conn.LastError,
			SubscriptionCount: conn.SubscriptionCount,
		}
		conn.mu.RUnlock()
		health = append(health, h)
	}
	return health
}

// ConnectionHealth represents the health of a single connection.
type ConnectionHealth struct {
	ConnID            int
	Connected         bool
	LastError         error
	SubscriptionCount int64
	LastMessageTime   time.Time
	IsStale           bool
}

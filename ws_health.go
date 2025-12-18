package hyperliquid

import (
	"sync"
	"time"
)

// SubscriptionHealth contains health information for a subscription.
type SubscriptionHealth struct {
	Key             string
	LastMessageTime time.Time
	MessageCount    int64
	IsStale         bool
}

// HealthConfig configures subscription health monitoring.
type HealthConfig struct {
	// StaleThreshold is how long without data before a subscription is considered stale.
	// Default: 30 seconds
	StaleThreshold time.Duration

	// CheckInterval is how often to check for stale subscriptions.
	// Default: 5 seconds
	CheckInterval time.Duration

	// OnStale is called when a subscription becomes stale (no data for StaleThreshold).
	// Called once per subscription when it transitions to stale state.
	OnStale func(health SubscriptionHealth)

	// OnHealthy is called when a stale subscription receives data again.
	OnHealthy func(health SubscriptionHealth)
}

// DefaultHealthConfig returns sensible defaults for health monitoring.
func DefaultHealthConfig() HealthConfig {
	return HealthConfig{
		StaleThreshold: 30 * time.Second,
		CheckInterval:  5 * time.Second,
	}
}

// healthMonitor tracks subscription health and detects stale subscriptions.
type healthMonitor struct {
	mu           sync.RWMutex
	config       HealthConfig
	staleSet     map[string]bool // tracks which subscriptions are currently stale
	done         chan struct{}
	getHealth    func() []SubscriptionHealth
	onReconnect  func() // callback to trigger reconnection
}

func newHealthMonitor(config HealthConfig, getHealth func() []SubscriptionHealth, onReconnect func()) *healthMonitor {
	if config.StaleThreshold == 0 {
		config.StaleThreshold = 30 * time.Second
	}
	if config.CheckInterval == 0 {
		config.CheckInterval = 5 * time.Second
	}

	return &healthMonitor{
		config:      config,
		staleSet:    make(map[string]bool),
		done:        make(chan struct{}),
		getHealth:   getHealth,
		onReconnect: onReconnect,
	}
}

func (h *healthMonitor) start() {
	go h.monitorLoop()
}

func (h *healthMonitor) stop() {
	close(h.done)
}

func (h *healthMonitor) monitorLoop() {
	ticker := time.NewTicker(h.config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-h.done:
			return
		case <-ticker.C:
			h.checkHealth()
		}
	}
}

func (h *healthMonitor) checkHealth() {
	healthList := h.getHealth()
	now := time.Now()

	h.mu.Lock()
	defer h.mu.Unlock()

	for _, health := range healthList {
		timeSinceLastMessage := now.Sub(health.LastMessageTime)
		wasStale := h.staleSet[health.Key]
		isNowStale := timeSinceLastMessage > h.config.StaleThreshold

		health.IsStale = isNowStale

		if isNowStale && !wasStale {
			// Transition to stale
			h.staleSet[health.Key] = true
			if h.config.OnStale != nil {
				// Call in goroutine to avoid blocking
				go h.config.OnStale(health)
			}
		} else if !isNowStale && wasStale {
			// Transition to healthy
			delete(h.staleSet, health.Key)
			if h.config.OnHealthy != nil {
				go h.config.OnHealthy(health)
			}
		}
	}
}

// markReceived is called when a subscription receives data.
// This is handled by uniqSubscriber directly now.
func (h *healthMonitor) markReceived(key string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// If it was stale, it will be detected as healthy on next check
	// No need to do anything here as uniqSubscriber tracks lastMessageTime
}


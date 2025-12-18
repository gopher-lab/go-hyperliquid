package hyperliquid

import (
	"os"
	"time"

	"github.com/sonirico/vago/lol"
)

type Opt[T any] func(*T)

func (o Opt[T]) Apply(opt *T) {
	o(opt)
}

type (
	ClientOpt   = Opt[client]
	ExchangeOpt = Opt[Exchange]
	InfoOpt     = Opt[Info]
	WsOpt       = Opt[WebsocketClient]
)

func WsOptDebugMode() WsOpt {
	return func(w *WebsocketClient) {
		w.debug = true
		w.logger = lol.NewZerolog(
			lol.WithLevel(lol.LevelTrace),
			lol.WithWriter(os.Stdout),
			lol.WithEnv(lol.EnvDev),
		)
	}
}

func InfoOptDebugMode() InfoOpt {
	return func(i *Info) {
		i.debug = true
	}
}

func ExchangeOptDebugMode() ExchangeOpt {
	return func(e *Exchange) {
		e.debug = true
	}
}

func clientOptDebugMode() ClientOpt {
	return func(c *client) {
		c.debug = true
		c.logger = lol.NewZerolog(
			lol.WithLevel(lol.LevelTrace),
			lol.WithWriter(os.Stderr),
			lol.WithEnv(lol.EnvDev),
		)
	}
}

// ExchangeOptClientOptions allows passing of ClientOpt to Client
func ExchangeOptClientOptions(opts ...ClientOpt) ExchangeOpt {
	return func(e *Exchange) {
		e.clientOpts = append(e.clientOpts, opts...)
	}
}

// ExchangeOptInfoOptions allows passing of InfoOpt to Info
func ExchangeOptInfoOptions(opts ...InfoOpt) ExchangeOpt {
	return func(e *Exchange) {
		e.infoOpts = append(e.infoOpts, opts...)
	}
}

// InfoOptClientOptions allows passing of ClientOpt to Info
func InfoOptClientOptions(opts ...ClientOpt) InfoOpt {
	return func(i *Info) {
		i.clientOpts = append(i.clientOpts, opts...)
	}
}

// WsOptOnDisconnect sets a callback that is called when the WebSocket disconnects.
func WsOptOnDisconnect(cb func(error)) WsOpt {
	return func(w *WebsocketClient) {
		w.onDisconnect = cb
	}
}

// WsOptOnReconnect sets a callback that is called when the WebSocket reconnects.
func WsOptOnReconnect(cb func()) WsOpt {
	return func(w *WebsocketClient) {
		w.onReconnect = cb
	}
}

// WsOptOnError sets a callback for non-fatal errors (e.g., message parse errors).
func WsOptOnError(cb func(error)) WsOpt {
	return func(w *WebsocketClient) {
		w.onError = cb
	}
}

// WsOptHealthConfig enables subscription health monitoring with the given config.
func WsOptHealthConfig(config HealthConfig) WsOpt {
	return func(w *WebsocketClient) {
		w.healthConfig = &config
	}
}

// WsOptOnStale sets a callback when a subscription becomes stale (no data received).
// This is a convenience wrapper around WsOptHealthConfig.
func WsOptOnStale(threshold time.Duration, cb func(SubscriptionHealth)) WsOpt {
	return func(w *WebsocketClient) {
		if w.healthConfig == nil {
			config := DefaultHealthConfig()
			w.healthConfig = &config
		}
		w.healthConfig.StaleThreshold = threshold
		w.healthConfig.OnStale = cb
	}
}

// WsOptOnHealthy sets a callback when a stale subscription becomes healthy again.
func WsOptOnHealthy(cb func(SubscriptionHealth)) WsOpt {
	return func(w *WebsocketClient) {
		if w.healthConfig == nil {
			config := DefaultHealthConfig()
			w.healthConfig = &config
		}
		w.healthConfig.OnHealthy = cb
	}
}

// WsOptReconnectWait sets the initial reconnect wait duration.
func WsOptReconnectWait(d time.Duration) WsOpt {
	return func(w *WebsocketClient) {
		w.reconnectWait = d
	}
}

// WsOptMaxReconnectWait sets the maximum reconnect wait duration (cap for exponential backoff).
func WsOptMaxReconnectWait(d time.Duration) WsOpt {
	return func(w *WebsocketClient) {
		w.maxReconnectWait = d
	}
}

// WsOptLogger sets a custom logger for the WebSocket client.
func WsOptLogger(logger lol.Logger) WsOpt {
	return func(w *WebsocketClient) {
		w.logger = logger
	}
}

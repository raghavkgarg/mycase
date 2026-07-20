package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/raghavkgarg/mycase/pkg/broker"
)

// SSEBroadcaster manages Server-Sent Events subscriptions and broadcasts live quotes.
type SSEBroadcaster struct {
	clients map[chan string]struct{}
	mu      sync.RWMutex
	tickers []string
	tickMu  sync.RWMutex
}

func newSSEBroadcaster() *SSEBroadcaster {
	return &SSEBroadcaster{
		clients: make(map[chan string]struct{}),
	}
}

// SetTickers updates the list of tickers to broadcast quotes for.
func (b *SSEBroadcaster) SetTickers(tickers []string) {
	b.tickMu.Lock()
	defer b.tickMu.Unlock()
	b.tickers = tickers
}

// Subscribe returns a channel that receives SSE messages and a cleanup func.
func (b *SSEBroadcaster) Subscribe() (chan string, func()) {
	ch := make(chan string, 64)
	b.mu.Lock()
	b.clients[ch] = struct{}{}
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		delete(b.clients, ch)
		b.mu.Unlock()
	}
}

// broadcast sends msg to all connected clients without blocking.
func (b *SSEBroadcaster) broadcast(msg string) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.clients {
		select {
		case ch <- msg:
		default:
		}
	}
}

// BroadcastLoop runs until ctx is cancelled, fetching quotes every interval.
func (b *SSEBroadcaster) BroadcastLoop(ctx context.Context, br broker.Broker, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.tickMu.RLock()
			tickers := b.tickers
			b.tickMu.RUnlock()
			if len(tickers) == 0 {
				continue
			}
			quotes, err := br.GetQuotes(tickers)
			if err != nil {
				continue
			}
			data, err := json.Marshal(quotes)
			if err != nil {
				continue
			}
			b.broadcast(fmt.Sprintf("data: %s\n\n", data))
		}
	}
}

// ServeSSE handles an SSE connection: subscribes the client and pushes messages until disconnect.
func (b *SSEBroadcaster) ServeSSE(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch, cleanup := b.Subscribe()
	defer cleanup()

	flusher, _ := w.(http.Flusher)

	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			fmt.Fprint(w, msg)
			if flusher != nil {
				flusher.Flush()
			}
		}
	}
}

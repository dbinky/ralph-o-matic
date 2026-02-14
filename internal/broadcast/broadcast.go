package broadcast

import (
	"sync"
	"sync/atomic"
)

const channelBufferSize = 16

// Broadcaster is an in-memory topic-based pub/sub for SSE events.
type Broadcaster struct {
	subscribers map[string]map[uint64]chan []byte
	nextID      uint64 // accessed via atomic
	mu          sync.RWMutex
}

// New creates a new Broadcaster.
func New() *Broadcaster {
	return &Broadcaster{
		subscribers: make(map[string]map[uint64]chan []byte),
	}
}

// Subscribe registers a client for a topic. Returns a client ID and a receive-only channel.
func (b *Broadcaster) Subscribe(topic string) (uint64, <-chan []byte) {
	id := atomic.AddUint64(&b.nextID, 1)
	ch := make(chan []byte, channelBufferSize)

	b.mu.Lock()
	if b.subscribers[topic] == nil {
		b.subscribers[topic] = make(map[uint64]chan []byte)
	}
	b.subscribers[topic][id] = ch
	b.mu.Unlock()

	return id, ch
}

// Unsubscribe removes a client from a topic.
func (b *Broadcaster) Unsubscribe(topic string, clientID uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	clients, ok := b.subscribers[topic]
	if !ok {
		return
	}

	if ch, exists := clients[clientID]; exists {
		close(ch)
		delete(clients, clientID)
	}
	if len(clients) == 0 {
		delete(b.subscribers, topic)
	}
}

// Publish sends data to all subscribers of a topic. Non-blocking: if a client's
// buffer is full, the event is dropped for that client.
func (b *Broadcaster) Publish(topic string, data []byte) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	clients, ok := b.subscribers[topic]
	if !ok {
		return
	}

	for _, ch := range clients {
		select {
		case ch <- data:
		default:
			// Client buffer full, drop event
		}
	}
}

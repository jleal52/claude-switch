// Package ring provides a fixed-capacity byte ring buffer for replaying
// the most recent output of a PTY session when the WebSocket reconnects.
package ring

import "sync"

type Buffer struct {
	mu   sync.Mutex
	data []byte
	cap  int
	head int // next write index in data[:cap]
	size int // bytes currently stored (≤ cap)
}

// New returns a ring buffer with fixed capacity.
func New(capacity int) *Buffer {
	return &Buffer{data: make([]byte, capacity), cap: capacity}
}

// Write appends bytes, evicting oldest bytes past capacity. If p is longer
// than cap, only the last cap bytes of p are retained.
func (b *Buffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(p) >= b.cap {
		copy(b.data, p[len(p)-b.cap:])
		b.head = 0
		b.size = b.cap
		return len(p), nil
	}

	for _, c := range p {
		b.data[b.head] = c
		b.head = (b.head + 1) % b.cap
		if b.size < b.cap {
			b.size++
		}
	}
	return len(p), nil
}

// Snapshot returns a copy of the current contents in chronological order.
func (b *Buffer) Snapshot() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()

	out := make([]byte, b.size)
	if b.size < b.cap {
		copy(out, b.data[:b.size])
		return out
	}
	// Buffer is full: oldest byte is at head, newest is at head-1.
	copy(out, b.data[b.head:])
	copy(out[b.cap-b.head:], b.data[:b.head])
	return out
}

// Len returns how many bytes are currently stored.
func (b *Buffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.size
}

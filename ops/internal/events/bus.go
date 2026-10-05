// Package events is an in-process pub/sub bus. The worker and agent handlers
// publish job state changes and log lines; the SSE handler subscribes per job.
// A slow subscriber drops events rather than blocking a publisher — the browser
// catches up from the database on reconnect.
package events

import "sync"

// Kind labels an event so the SSE layer can route it to the right swap.
type Kind string

const (
	KindLog    Kind = "log"    // a new log line (HTML fragment built by the handler)
	KindStatus Kind = "status" // a job's new status
	KindDone   Kind = "done"   // the job finished; close the stream
)

// Event is one published message for a job.
type Event struct {
	JobID  int64
	Kind   Kind
	Status string // for KindStatus
	// Log fields (for KindLog):
	Seq    int64
	Stream string
	Line   string
}

// Bus fans events out to per-job subscribers.
type Bus struct {
	mu   sync.Mutex
	subs map[int64]map[chan Event]struct{}
}

// New builds an empty Bus.
func New() *Bus {
	return &Bus{subs: make(map[int64]map[chan Event]struct{})}
}

// Subscribe returns a buffered channel of events for a job and an unsubscribe
// func the caller must defer.
func (b *Bus) Subscribe(jobID int64) (<-chan Event, func()) {
	ch := make(chan Event, 64)
	b.mu.Lock()
	m := b.subs[jobID]
	if m == nil {
		m = make(map[chan Event]struct{})
		b.subs[jobID] = m
	}
	m[ch] = struct{}{}
	b.mu.Unlock()

	return ch, func() {
		b.mu.Lock()
		if m := b.subs[jobID]; m != nil {
			if _, ok := m[ch]; ok {
				delete(m, ch)
				close(ch)
			}
			if len(m) == 0 {
				delete(b.subs, jobID)
			}
		}
		b.mu.Unlock()
	}
}

// Publish delivers e to every subscriber of e.JobID, dropping to a subscriber
// whose buffer is full rather than blocking.
func (b *Bus) Publish(e Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs[e.JobID] {
		select {
		case ch <- e:
		default: // slow subscriber: drop, it will catch up from the DB
		}
	}
}

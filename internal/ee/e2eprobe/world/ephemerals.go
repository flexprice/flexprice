package world

import (
	"sync"
	"time"
)

// Entry is one ephemeral resource the janitor will remove once it is old enough.
type Entry struct {
	Kind      string
	ID        string
	CreatedAt time.Time
}

// Ephemerals is the in-memory registry of resources scenarios created.
type Ephemerals struct {
	mu      sync.Mutex
	entries map[string][]Entry
}

func NewEphemerals() *Ephemerals {
	return &Ephemerals{entries: map[string][]Entry{}}
}

func (e *Ephemerals) Register(kind, id string, at time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.entries[kind] = append(e.entries[kind], Entry{Kind: kind, ID: id, CreatedAt: at})
}

func (e *Ephemerals) List(kind string) []Entry {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]Entry(nil), e.entries[kind]...)
}

func (e *Ephemerals) Archive(kind, id string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	kept := e.entries[kind][:0]
	for _, en := range e.entries[kind] {
		if en.ID != id {
			kept = append(kept, en)
		}
	}
	e.entries[kind] = kept
}

func (e *Ephemerals) Count(kind string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.entries[kind])
}

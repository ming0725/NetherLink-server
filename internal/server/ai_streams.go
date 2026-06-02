package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
)

type aiStreamRegistry struct {
	mu      sync.Mutex
	streams map[string]aiStreamEntry
}

type aiStreamEntry struct {
	userID       string
	connectionID string
	cancel       context.CancelFunc
}

func newAIStreamRegistry() *aiStreamRegistry {
	return &aiStreamRegistry{streams: map[string]aiStreamEntry{}}
}

func (r *aiStreamRegistry) Add(userID string, connectionID string, cancel context.CancelFunc) string {
	if r == nil || cancel == nil {
		return ""
	}
	streamID := newAIStreamID()
	r.mu.Lock()
	r.streams[streamID] = aiStreamEntry{userID: userID, connectionID: connectionID, cancel: cancel}
	r.mu.Unlock()
	return streamID
}

func (r *aiStreamRegistry) Remove(streamID string) {
	if r == nil || streamID == "" {
		return
	}
	r.mu.Lock()
	delete(r.streams, streamID)
	r.mu.Unlock()
}

func (r *aiStreamRegistry) Cancel(streamID string, userID string) bool {
	if r == nil || streamID == "" {
		return false
	}
	r.mu.Lock()
	entry, ok := r.streams[streamID]
	if ok && entry.userID == userID {
		delete(r.streams, streamID)
	}
	r.mu.Unlock()
	if !ok || entry.userID != userID {
		return false
	}
	entry.cancel()
	return true
}

func (r *aiStreamRegistry) CancelConnection(connectionID string) {
	if r == nil || connectionID == "" {
		return
	}
	var cancels []context.CancelFunc
	r.mu.Lock()
	for streamID, entry := range r.streams {
		if entry.connectionID == connectionID {
			cancels = append(cancels, entry.cancel)
			delete(r.streams, streamID)
		}
	}
	r.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

func newAIStreamID() string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "ais_unavailable"
	}
	return "ais_" + hex.EncodeToString(bytes[:])
}

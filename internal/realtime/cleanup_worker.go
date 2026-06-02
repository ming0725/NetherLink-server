package realtime

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"nlserver/internal/repository"
)

type CleanupWorker struct {
	repo      *repository.RealtimeEventRepository
	retention time.Duration
	interval  time.Duration
	batchSize int
	logger    *slog.Logger

	deletedEvents atomic.Uint64
	runFailures   atomic.Uint64

	mu           sync.Mutex
	lastRunAt    time.Time
	lastDeleted  int64
	lastErrorAt  time.Time
	lastError    string
	lastBeforeAt time.Time
}

type CleanupSnapshot struct {
	Enabled       bool      `json:"enabled"`
	Retention     string    `json:"retention"`
	Interval      string    `json:"interval"`
	BatchSize     int       `json:"batchSize"`
	DeletedEvents uint64    `json:"deletedEvents"`
	RunFailures   uint64    `json:"runFailures"`
	LastRunAt     time.Time `json:"lastRunAt,omitempty"`
	LastDeleted   int64     `json:"lastDeleted"`
	LastErrorAt   time.Time `json:"lastErrorAt,omitempty"`
	LastError     string    `json:"lastError,omitempty"`
	LastBeforeAt  time.Time `json:"lastBeforeAt,omitempty"`
}

func NewCleanupWorker(repo *repository.RealtimeEventRepository, retention time.Duration, interval time.Duration, batchSize int, logger *slog.Logger) *CleanupWorker {
	if logger == nil {
		logger = slog.Default()
	}
	if retention <= 0 {
		retention = 7 * 24 * time.Hour
	}
	if interval <= 0 {
		interval = time.Hour
	}
	if batchSize <= 0 {
		batchSize = 5000
	}

	return &CleanupWorker{
		repo:      repo,
		retention: retention,
		interval:  interval,
		batchSize: batchSize,
		logger:    logger,
	}
}

func (w *CleanupWorker) Start(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)

		w.run(ctx)
		ticker := time.NewTicker(w.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				w.run(ctx)
			}
		}
	}()
	return done
}

func (w *CleanupWorker) Snapshot() CleanupSnapshot {
	if w == nil || w.repo == nil {
		return CleanupSnapshot{}
	}

	w.mu.Lock()
	lastRunAt := w.lastRunAt
	lastDeleted := w.lastDeleted
	lastErrorAt := w.lastErrorAt
	lastError := w.lastError
	lastBeforeAt := w.lastBeforeAt
	w.mu.Unlock()

	return CleanupSnapshot{
		Enabled:       true,
		Retention:     w.retention.String(),
		Interval:      w.interval.String(),
		BatchSize:     w.batchSize,
		DeletedEvents: w.deletedEvents.Load(),
		RunFailures:   w.runFailures.Load(),
		LastRunAt:     lastRunAt,
		LastDeleted:   lastDeleted,
		LastErrorAt:   lastErrorAt,
		LastError:     lastError,
		LastBeforeAt:  lastBeforeAt,
	}
}

func (w *CleanupWorker) run(ctx context.Context) {
	if w == nil || w.repo == nil {
		return
	}

	before := time.Now().UTC().Add(-w.retention)
	deleted, err := w.repo.DeleteBefore(ctx, before, w.batchSize)
	now := time.Now().UTC()

	w.mu.Lock()
	w.lastRunAt = now
	w.lastBeforeAt = before
	w.lastDeleted = deleted
	if err != nil {
		w.lastErrorAt = now
		w.lastError = err.Error()
	}
	w.mu.Unlock()

	if err != nil {
		w.runFailures.Add(1)
		w.logger.WarnContext(ctx, "realtime event cleanup failed", "error", err)
		return
	}
	if deleted > 0 {
		w.deletedEvents.Add(uint64(deleted))
		w.logger.InfoContext(ctx, "realtime events cleaned", "deleted", deleted, "before", before)
	}
}

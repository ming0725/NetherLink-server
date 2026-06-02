package files

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"nlserver/internal/repository"
	"nlserver/internal/storage"
)

const (
	defaultCleanupPollInterval = 1 * time.Hour
	defaultCleanupBatchSize    = 25
	cleanupClaimRetryAge       = 15 * time.Minute
	unusedFileRetention        = 24 * time.Hour
)

type CleanupWorker struct {
	repos   repository.Repositories
	storage storage.ObjectStorageProvider
	logger  *slog.Logger
}

func NewCleanupWorker(repos repository.Repositories, provider storage.ObjectStorageProvider, logger *slog.Logger) *CleanupWorker {
	if logger == nil {
		logger = slog.Default()
	}
	return &CleanupWorker{
		repos:   repos,
		storage: provider,
		logger:  logger,
	}
}

func (w *CleanupWorker) Start(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		w.run(ctx)
	}()
	return done
}

func (w *CleanupWorker) RunOnce(ctx context.Context) (bool, error) {
	if w == nil || w.repos.Files == nil || w.storage == nil {
		return false, ErrStorageUnavailable
	}

	expiredCount, err := w.cleanupExpiredUploadSessions(ctx)
	if err != nil {
		return false, err
	}

	unusedCount, err := w.cleanupUnusedFiles(ctx)
	if err != nil {
		return expiredCount > 0, err
	}

	return expiredCount+unusedCount > 0, nil
}

func (w *CleanupWorker) run(ctx context.Context) {
	ticker := time.NewTicker(defaultCleanupPollInterval)
	defer ticker.Stop()

	for {
		cleaned, err := w.RunOnce(ctx)
		if err != nil {
			w.logger.Error("file cleanup worker error", "error", err)
		} else if cleaned {
			continue
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *CleanupWorker) cleanupExpiredUploadSessions(ctx context.Context) (int, error) {
	candidates, err := w.repos.Files.ClaimExpiredUploadSessionsForCleanup(ctx, defaultCleanupBatchSize, cleanupClaimRetryAge)
	if err != nil {
		return 0, err
	}

	cleaned := 0
	for _, candidate := range candidates {
		if err := w.cleanupExpiredUploadSession(ctx, candidate); err != nil {
			w.logger.Error(
				"expired upload session cleanup failed",
				"session_id", candidate.SessionID,
				"file_id", candidate.FileID,
				"upload_mode", candidate.UploadMode,
				"error", err,
			)
			continue
		}
		cleaned++
	}
	return cleaned, nil
}

func (w *CleanupWorker) cleanupExpiredUploadSession(ctx context.Context, candidate repository.ExpiredUploadSessionCleanupCandidate) error {
	if candidate.UploadMode == uploadModeMultipart && candidate.S3UploadID != nil {
		if err := w.storage.AbortMultipartUpload(ctx, storage.AbortMultipartUploadInput{
			Bucket:    candidate.Bucket,
			ObjectKey: candidate.ObjectKey,
			UploadID:  *candidate.S3UploadID,
		}); err != nil {
			return err
		}
	} else {
		if err := w.storage.DeleteObject(ctx, storage.DeleteObjectInput{
			Bucket:    candidate.Bucket,
			ObjectKey: candidate.ObjectKey,
		}); err != nil {
			return err
		}
	}

	return w.repos.Files.MarkExpiredUploadSessionCleaned(ctx, candidate.SessionID, candidate.FileID)
}

func (w *CleanupWorker) cleanupUnusedFiles(ctx context.Context) (int, error) {
	candidates, err := w.repos.Files.ClaimUnusedFilesForCleanup(ctx, unusedFileRetention, defaultCleanupBatchSize, cleanupClaimRetryAge)
	if err != nil {
		return 0, err
	}

	cleaned := 0
	for _, candidate := range candidates {
		if err := w.cleanupUnusedFile(ctx, candidate); err != nil {
			w.logger.Error("unused file cleanup failed", "file_id", candidate.FileID, "error", err)
			continue
		}
		cleaned++
	}
	return cleaned, nil
}

func (w *CleanupWorker) cleanupUnusedFile(ctx context.Context, candidate repository.UnusedFileCleanupCandidate) error {
	refs := append([]repository.FileObjectReference{}, candidate.Variants...)
	refs = append(refs, repository.FileObjectReference{
		Bucket:    candidate.Bucket,
		ObjectKey: candidate.ObjectKey,
	})

	seen := map[repository.FileObjectReference]struct{}{}
	for _, ref := range refs {
		if ref.Bucket == "" || ref.ObjectKey == "" {
			continue
		}
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}

		if err := w.storage.DeleteObject(ctx, storage.DeleteObjectInput{
			Bucket:    ref.Bucket,
			ObjectKey: ref.ObjectKey,
		}); err != nil {
			return err
		}
	}

	if err := w.repos.Files.MarkUnusedFileCleaned(ctx, candidate.FileID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil
		}
		return err
	}
	return nil
}

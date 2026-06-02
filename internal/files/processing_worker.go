package files

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"log/slog"
	"math"
	"path/filepath"
	"strings"
	"time"

	"nlserver/internal/repository"
	"nlserver/internal/storage"
)

const (
	defaultProcessingPollInterval = 5 * time.Second
	thumbnailMaxSide              = 512
	maxThumbnailSourceBytes       = 32 * 1024 * 1024
)

type ProcessingWorker struct {
	repos   repository.Repositories
	db      repository.TxBeginner
	storage storage.ObjectStorageProvider
	logger  *slog.Logger
}

type imageProcessingResult struct {
	Width              int     `json:"width"`
	Height             int     `json:"height"`
	Format             string  `json:"format"`
	ThumbnailVariantID *string `json:"thumbnailVariantId,omitempty"`
	ThumbnailSkipped   *string `json:"thumbnailSkipped,omitempty"`
}

func NewProcessingWorker(repos repository.Repositories, db repository.TxBeginner, provider storage.ObjectStorageProvider, logger *slog.Logger) *ProcessingWorker {
	if logger == nil {
		logger = slog.Default()
	}
	return &ProcessingWorker{
		repos:   repos,
		db:      db,
		storage: provider,
		logger:  logger,
	}
}

func (w *ProcessingWorker) Start(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		w.run(ctx)
	}()
	return done
}

func (w *ProcessingWorker) RunOnce(ctx context.Context) (bool, error) {
	if w == nil || w.repos.Files == nil || w.db == nil || w.storage == nil {
		return false, ErrStorageUnavailable
	}

	job, err := w.repos.Files.ClaimNextProcessingJob(ctx)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return false, nil
		}
		return false, err
	}

	if err := w.processJob(ctx, job); err != nil {
		w.logger.Error("file processing job failed", "job_id", job.ID, "file_id", job.FileID, "job_type", job.JobType, "error", err)
		return true, nil
	}
	return true, nil
}

func (w *ProcessingWorker) run(ctx context.Context) {
	ticker := time.NewTicker(defaultProcessingPollInterval)
	defer ticker.Stop()

	for {
		for {
			processed, err := w.RunOnce(ctx)
			if err != nil {
				w.logger.Error("file processing worker error", "error", err)
				break
			}
			if !processed {
				break
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *ProcessingWorker) processJob(ctx context.Context, job repository.FileProcessingJob) error {
	file, err := w.repos.Files.Get(ctx, job.FileID)
	if err != nil {
		return w.failJob(ctx, job, "FILE_NOT_FOUND", map[string]string{"error": err.Error()}, false)
	}

	switch job.JobType {
	case fileProcessingJobImage:
		return w.processImage(ctx, job, file)
	default:
		return w.failJob(ctx, job, "UNSUPPORTED_JOB_TYPE", map[string]string{"jobType": job.JobType}, false)
	}
}

func (w *ProcessingWorker) processImage(ctx context.Context, job repository.FileProcessingJob, file repository.File) error {
	reader, err := w.storage.GetObject(ctx, storage.GetObjectInput{
		Bucket:    file.Bucket,
		ObjectKey: file.ObjectKey,
	})
	if err != nil {
		return w.failJob(ctx, job, "OBJECT_READ_FAILED", map[string]string{"error": err.Error()}, true)
	}
	cfg, format, err := image.DecodeConfig(reader.Body)
	_ = reader.Body.Close()
	if err != nil {
		return w.failJob(ctx, job, "IMAGE_METADATA_FAILED", map[string]string{"error": err.Error()}, true)
	}

	result := imageProcessingResult{
		Width:  cfg.Width,
		Height: cfg.Height,
		Format: format,
	}

	if cfg.Width <= thumbnailMaxSide && cfg.Height <= thumbnailMaxSide {
		value := "source_within_thumbnail_bounds"
		result.ThumbnailSkipped = &value
		return w.finishImageJob(ctx, job, file, result, nil)
	}
	if file.SizeBytes > maxThumbnailSourceBytes {
		value := "source_too_large"
		result.ThumbnailSkipped = &value
		return w.finishImageJob(ctx, job, file, result, nil)
	}

	variant, err := w.createImageThumbnail(ctx, file, cfg.Width, cfg.Height)
	if err != nil {
		value := "thumbnail_failed"
		result.ThumbnailSkipped = &value
		w.logger.Warn("file thumbnail generation failed", "file_id", file.ID, "error", err)
		return w.finishImageJob(ctx, job, file, result, nil)
	}

	result.ThumbnailVariantID = &variant.ID
	return w.finishImageJob(ctx, job, file, result, &variant)
}

func (w *ProcessingWorker) createImageThumbnail(ctx context.Context, file repository.File, width int, height int) (repository.FileVariant, error) {
	reader, err := w.storage.GetObject(ctx, storage.GetObjectInput{
		Bucket:    file.Bucket,
		ObjectKey: file.ObjectKey,
	})
	if err != nil {
		return repository.FileVariant{}, err
	}
	source, _, err := image.Decode(reader.Body)
	_ = reader.Body.Close()
	if err != nil {
		return repository.FileVariant{}, err
	}

	thumbnail := resizeNearest(source, width, height, thumbnailMaxSide)
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, thumbnail, &jpeg.Options{Quality: 82}); err != nil {
		return repository.FileVariant{}, err
	}

	objectKey := thumbnailObjectKey(file.ObjectKey)
	output, err := w.storage.PutObject(ctx, storage.PutObjectInput{
		Bucket:      file.Bucket,
		ObjectKey:   objectKey,
		Body:        bytes.NewReader(encoded.Bytes()),
		SizeBytes:   int64(encoded.Len()),
		ContentType: "image/jpeg",
	})
	if err != nil {
		return repository.FileVariant{}, err
	}

	sizeBytes := int64(encoded.Len())
	thumbWidth := thumbnail.Bounds().Dx()
	thumbHeight := thumbnail.Bounds().Dy()
	return repository.FileVariant{
		FileID:      file.ID,
		VariantType: "thumbnail_512",
		Bucket:      file.Bucket,
		ObjectKey:   objectKey,
		MimeType:    "image/jpeg",
		SizeBytes:   sizeBytes,
		Width:       &thumbWidth,
		Height:      &thumbHeight,
		ETag:        &output.ETag,
	}, nil
}

func (w *ProcessingWorker) finishImageJob(ctx context.Context, job repository.FileProcessingJob, file repository.File, result imageProcessingResult, variant *repository.FileVariant) error {
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return err
	}

	return repository.WithTx(ctx, w.db, func(repos repository.Repositories) error {
		var storedVariant *repository.FileVariant
		if variant != nil {
			created, err := repos.Files.CreateVariant(ctx, repository.CreateFileVariantParams{
				FileID:      file.ID,
				VariantType: variant.VariantType,
				Bucket:      variant.Bucket,
				ObjectKey:   variant.ObjectKey,
				MimeType:    variant.MimeType,
				SizeBytes:   variant.SizeBytes,
				Width:       variant.Width,
				Height:      variant.Height,
				DurationMS:  variant.DurationMS,
				ETag:        variant.ETag,
				ContentHash: variant.ContentHash,
			})
			if err != nil {
				return err
			}
			storedVariant = &created
		}

		if storedVariant != nil {
			result.ThumbnailVariantID = &storedVariant.ID
			resultJSON, err = json.Marshal(result)
			if err != nil {
				return err
			}
		}

		if _, err := repos.Files.MarkProcessingJobSucceeded(ctx, job.ID, resultJSON); err != nil {
			return err
		}
		_, err := repos.Files.MarkStatus(ctx, file.ID, fileStatusReady)
		return err
	})
}

func (w *ProcessingWorker) failJob(ctx context.Context, job repository.FileProcessingJob, errorCode string, payload any, markFileFailed bool) error {
	result, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	return repository.WithTx(ctx, w.db, func(repos repository.Repositories) error {
		if _, err := repos.Files.MarkProcessingJobFailed(ctx, job.ID, errorCode, result); err != nil {
			return err
		}
		if markFileFailed {
			_, err := repos.Files.MarkStatus(ctx, job.FileID, "failed")
			return err
		}
		return nil
	})
}

func resizeNearest(source image.Image, width int, height int, maxSide int) *image.RGBA {
	scale := float64(maxSide) / float64(max(width, height))
	targetWidth := max(1, int(math.Round(float64(width)*scale)))
	targetHeight := max(1, int(math.Round(float64(height)*scale)))
	target := image.NewRGBA(image.Rect(0, 0, targetWidth, targetHeight))

	bounds := source.Bounds()
	for y := 0; y < targetHeight; y++ {
		sourceY := bounds.Min.Y + int(float64(y)*float64(height)/float64(targetHeight))
		for x := 0; x < targetWidth; x++ {
			sourceX := bounds.Min.X + int(float64(x)*float64(width)/float64(targetWidth))
			target.Set(x, y, source.At(sourceX, sourceY))
		}
	}

	return target
}

func thumbnailObjectKey(sourceObjectKey string) string {
	dir := filepath.ToSlash(filepath.Dir(strings.TrimSpace(sourceObjectKey)))
	if dir == "." || dir == "/" {
		dir = "variants"
	}
	return strings.TrimRight(dir, "/") + "/thumbnail_512.jpg"
}

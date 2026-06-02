package files

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"log/slog"
	"math"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"nlserver/internal/repository"
	"nlserver/internal/storage"
)

const (
	defaultProcessingPollInterval = 5 * time.Second
	thumbnailMaxSide              = 512
	maxThumbnailSourceBytes       = 32 * 1024 * 1024
	maxMediaProbeSourceBytes      = 256 * 1024 * 1024
	mediaCommandTimeout           = 45 * time.Second
	staleProcessingJobAge         = 15 * time.Minute
	maxAIParseSourceBytes         = 4 * 1024 * 1024
	aiParseChunkRunes             = 2400
	aiParseChunkOverlapRunes      = 200
	maxAIParseChunks              = 80
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

type videoProcessingResult struct {
	Width          *int    `json:"width,omitempty"`
	Height         *int    `json:"height,omitempty"`
	DurationMS     *int    `json:"durationMs,omitempty"`
	FormatName     string  `json:"formatName,omitempty"`
	CoverVariantID *string `json:"coverVariantId,omitempty"`
}

type audioProcessingResult struct {
	DurationMS *int   `json:"durationMs,omitempty"`
	FormatName string `json:"formatName,omitempty"`
}

type aiParseResult struct {
	UploadedFileIDs []string `json:"uploadedFileIds"`
	ChunkCount      int      `json:"chunkCount"`
	TokenCount      int      `json:"tokenCount"`
	Truncated       bool     `json:"truncated"`
}

type ffprobeOutput struct {
	Streams []struct {
		CodecType string `json:"codec_type"`
		Width     int    `json:"width"`
		Height    int    `json:"height"`
		Duration  string `json:"duration"`
	} `json:"streams"`
	Format struct {
		Duration   string `json:"duration"`
		FormatName string `json:"format_name"`
	} `json:"format"`
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

	reset, err := w.repos.Files.ResetStaleProcessingJobs(ctx, staleProcessingJobAge)
	if err != nil {
		return false, err
	}
	if reset > 0 {
		w.logger.Warn("reset stale file processing jobs", "count", reset, "stale_age", staleProcessingJobAge.String())
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
	case fileProcessingJobVideo:
		return w.processVideo(ctx, job, file)
	case fileProcessingJobAudio:
		return w.processAudio(ctx, job, file)
	case "ai_parse":
		return w.processAIParse(ctx, job, file)
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

func (w *ProcessingWorker) processVideo(ctx context.Context, job repository.FileProcessingJob, file repository.File) error {
	data, err := w.readObjectBytesLimited(ctx, file, maxMediaProbeSourceBytes)
	if err != nil {
		return w.failJob(ctx, job, "VIDEO_OBJECT_READ_FAILED", map[string]string{"error": err.Error()}, true)
	}

	probe, err := probeMedia(ctx, data)
	if err != nil {
		return w.failJob(ctx, job, "VIDEO_METADATA_FAILED", map[string]string{"error": err.Error()}, true)
	}

	result := videoProcessingResult{
		FormatName: probe.Format.FormatName,
	}
	for _, stream := range probe.Streams {
		if stream.CodecType == "video" {
			if stream.Width > 0 {
				width := stream.Width
				result.Width = &width
			}
			if stream.Height > 0 {
				height := stream.Height
				result.Height = &height
			}
			if duration := durationMilliseconds(firstNonEmpty(stream.Duration, probe.Format.Duration)); duration != nil {
				result.DurationMS = duration
			}
			break
		}
	}
	if result.DurationMS == nil {
		result.DurationMS = durationMilliseconds(probe.Format.Duration)
	}

	variant, err := w.createVideoCover(ctx, file, data)
	if err != nil {
		return w.failJob(ctx, job, "VIDEO_COVER_FAILED", map[string]string{"error": err.Error()}, true)
	}

	result.CoverVariantID = &variant.ID
	return w.finishVideoJob(ctx, job, file, result, &variant)
}

func (w *ProcessingWorker) processAudio(ctx context.Context, job repository.FileProcessingJob, file repository.File) error {
	data, err := w.readObjectBytesLimited(ctx, file, maxMediaProbeSourceBytes)
	if err != nil {
		return w.failJob(ctx, job, "AUDIO_OBJECT_READ_FAILED", map[string]string{"error": err.Error()}, true)
	}

	probe, err := probeMedia(ctx, data)
	if err != nil {
		return w.failJob(ctx, job, "AUDIO_METADATA_FAILED", map[string]string{"error": err.Error()}, true)
	}

	result := audioProcessingResult{
		FormatName: probe.Format.FormatName,
	}
	for _, stream := range probe.Streams {
		if stream.CodecType == "audio" {
			result.DurationMS = durationMilliseconds(firstNonEmpty(stream.Duration, probe.Format.Duration))
			break
		}
	}
	if result.DurationMS == nil {
		result.DurationMS = durationMilliseconds(probe.Format.Duration)
	}
	if result.DurationMS == nil {
		return w.failJob(ctx, job, "AUDIO_DURATION_MISSING", map[string]string{"error": "ffprobe did not return a duration"}, true)
	}

	return w.finishAudioJob(ctx, job, file, result)
}

func (w *ProcessingWorker) processAIParse(ctx context.Context, job repository.FileProcessingJob, file repository.File) error {
	if w.repos.AI == nil {
		return w.failJob(ctx, job, "AI_REPOSITORY_UNAVAILABLE", map[string]string{"jobType": job.JobType}, false)
	}

	uploadedFiles, err := w.repos.AI.ListUploadedFilesByFileID(ctx, file.ID)
	if err != nil {
		return w.failAIParseJob(ctx, job, file.ID, "AI_UPLOAD_LOOKUP_FAILED", map[string]string{"error": err.Error()})
	}
	if len(uploadedFiles) == 0 {
		result := aiParseResult{UploadedFileIDs: []string{}}
		return w.finishAIParseJob(ctx, job, file.ID, uploadedFiles, nil, result)
	}
	if err := w.repos.AI.SetUploadedFilesParseStatusByFileID(ctx, file.ID, "processing", 0); err != nil {
		return w.failAIParseJob(ctx, job, file.ID, "AI_PARSE_STATUS_FAILED", map[string]string{"error": err.Error()})
	}

	data, err := w.readObjectBytesLimited(ctx, file, maxAIParseSourceBytes)
	if err != nil {
		return w.failAIParseJob(ctx, job, file.ID, "AI_OBJECT_READ_FAILED", map[string]string{"error": err.Error()})
	}
	text, err := extractAIText(file, data)
	if err != nil {
		return w.failAIParseJob(ctx, job, file.ID, "AI_PARSE_UNSUPPORTED", map[string]string{"error": err.Error(), "mimeType": file.MimeType})
	}

	chunks, truncated := chunkAIText(text)
	tokenCount := roughTextTokenCount(text)
	uploadedFileIDs := make([]string, 0, len(uploadedFiles))
	for _, uploadedFile := range uploadedFiles {
		uploadedFileIDs = append(uploadedFileIDs, uploadedFile.ID)
	}
	result := aiParseResult{
		UploadedFileIDs: uploadedFileIDs,
		ChunkCount:      len(chunks),
		TokenCount:      tokenCount,
		Truncated:       truncated,
	}
	return w.finishAIParseJob(ctx, job, file.ID, uploadedFiles, chunks, result)
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

func (w *ProcessingWorker) createVideoCover(ctx context.Context, file repository.File, data []byte) (repository.FileVariant, error) {
	output, err := runMediaCommand(ctx, "ffmpeg",
		"-hide_banner",
		"-loglevel", "error",
		"-i", "pipe:0",
		"-frames:v", "1",
		"-vf", fmt.Sprintf("scale=w='min(%d,iw)':h=-2", thumbnailMaxSide),
		"-f", "image2pipe",
		"-vcodec", "mjpeg",
		"pipe:1",
		data,
	)
	if err != nil {
		return repository.FileVariant{}, err
	}
	if len(output) == 0 {
		return repository.FileVariant{}, errors.New("ffmpeg returned an empty cover image")
	}

	cfg, _, err := image.DecodeConfig(bytes.NewReader(output))
	if err != nil {
		return repository.FileVariant{}, err
	}

	objectKey := videoCoverObjectKey(file.ObjectKey)
	put, err := w.storage.PutObject(ctx, storage.PutObjectInput{
		Bucket:      file.Bucket,
		ObjectKey:   objectKey,
		Body:        bytes.NewReader(output),
		SizeBytes:   int64(len(output)),
		ContentType: "image/jpeg",
	})
	if err != nil {
		return repository.FileVariant{}, err
	}

	width := cfg.Width
	height := cfg.Height
	return repository.FileVariant{
		FileID:      file.ID,
		VariantType: "video_cover_512",
		Bucket:      file.Bucket,
		ObjectKey:   objectKey,
		MimeType:    "image/jpeg",
		SizeBytes:   int64(len(output)),
		Width:       &width,
		Height:      &height,
		ETag:        &put.ETag,
	}, nil
}

func (w *ProcessingWorker) finishVideoJob(ctx context.Context, job repository.FileProcessingJob, file repository.File, result videoProcessingResult, variant *repository.FileVariant) error {
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return err
	}

	return repository.WithTx(ctx, w.db, func(repos repository.Repositories) error {
		created, err := repos.Files.CreateVariant(ctx, repository.CreateFileVariantParams{
			FileID:      file.ID,
			VariantType: variant.VariantType,
			Bucket:      variant.Bucket,
			ObjectKey:   variant.ObjectKey,
			MimeType:    variant.MimeType,
			SizeBytes:   variant.SizeBytes,
			Width:       variant.Width,
			Height:      variant.Height,
			DurationMS:  result.DurationMS,
			ETag:        variant.ETag,
			ContentHash: variant.ContentHash,
		})
		if err != nil {
			return err
		}

		result.CoverVariantID = &created.ID
		resultJSON, err = json.Marshal(result)
		if err != nil {
			return err
		}

		if _, err := repos.Files.MarkProcessingJobSucceeded(ctx, job.ID, resultJSON); err != nil {
			return err
		}
		_, err = repos.Files.MarkStatus(ctx, file.ID, fileStatusReady)
		return err
	})
}

func (w *ProcessingWorker) finishAudioJob(ctx context.Context, job repository.FileProcessingJob, file repository.File, result audioProcessingResult) error {
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return err
	}

	return repository.WithTx(ctx, w.db, func(repos repository.Repositories) error {
		if _, err := repos.Files.MarkProcessingJobSucceeded(ctx, job.ID, resultJSON); err != nil {
			return err
		}
		_, err := repos.Files.MarkStatus(ctx, file.ID, fileStatusReady)
		return err
	})
}

func (w *ProcessingWorker) finishAIParseJob(ctx context.Context, job repository.FileProcessingJob, fileID string, uploadedFiles []repository.AIUploadedFile, chunkTexts []string, result aiParseResult) error {
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return err
	}

	return repository.WithTx(ctx, w.db, func(repos repository.Repositories) error {
		if repos.AI == nil {
			return errors.New("ai repository unavailable")
		}
		for _, uploadedFile := range uploadedFiles {
			chunks := make([]repository.CreateAIFileChunkParams, 0, len(chunkTexts))
			for index, content := range chunkTexts {
				metadata, err := json.Marshal(map[string]any{
					"sourceFileId": fileID,
					"chunkIndex":   index,
				})
				if err != nil {
					return err
				}
				chunks = append(chunks, repository.CreateAIFileChunkParams{
					AIFileID:   uploadedFile.ID,
					ChunkIndex: index,
					Content:    content,
					TokenCount: roughTextTokenCount(content),
					Metadata:   metadata,
				})
			}
			if err := repos.AI.ReplaceFileChunks(ctx, uploadedFile.ID, chunks); err != nil {
				return err
			}
			if _, err := repos.AI.SetUploadedFileParseStatus(ctx, uploadedFile.ID, "ready", result.TokenCount); err != nil {
				return err
			}
		}
		_, err := repos.Files.MarkProcessingJobSucceeded(ctx, job.ID, resultJSON)
		return err
	})
}

func (w *ProcessingWorker) failAIParseJob(ctx context.Context, job repository.FileProcessingJob, fileID string, errorCode string, payload any) error {
	result, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	return repository.WithTx(ctx, w.db, func(repos repository.Repositories) error {
		if repos.AI != nil {
			if err := repos.AI.SetUploadedFilesParseStatusByFileID(ctx, fileID, "failed", 0); err != nil {
				return err
			}
		}
		_, err := repos.Files.MarkProcessingJobFailed(ctx, job.ID, errorCode, result)
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
			_, err := repos.Files.MarkStatus(ctx, job.FileID, fileStatusFailed)
			return err
		}
		return nil
	})
}

func (w *ProcessingWorker) readObjectBytesLimited(ctx context.Context, file repository.File, maxBytes int64) ([]byte, error) {
	reader, err := w.storage.GetObject(ctx, storage.GetObjectInput{
		Bucket:    file.Bucket,
		ObjectKey: file.ObjectKey,
	})
	if err != nil {
		return nil, err
	}
	defer reader.Body.Close()

	limited := io.LimitReader(reader.Body, maxBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("object exceeds media processing limit of %d bytes", maxBytes)
	}
	return data, nil
}

func probeMedia(ctx context.Context, data []byte) (ffprobeOutput, error) {
	output, err := runMediaCommand(ctx, "ffprobe",
		"-v", "error",
		"-show_streams",
		"-show_entries", "format=duration,format_name",
		"-of", "json",
		"pipe:0",
		data,
	)
	if err != nil {
		return ffprobeOutput{}, err
	}

	var probe ffprobeOutput
	if err := json.Unmarshal(output, &probe); err != nil {
		return ffprobeOutput{}, err
	}
	return probe, nil
}

func runMediaCommand(ctx context.Context, name string, argsAndInput ...any) ([]byte, error) {
	if len(argsAndInput) == 0 {
		return nil, errors.New("media command input missing")
	}
	input, ok := argsAndInput[len(argsAndInput)-1].([]byte)
	if !ok {
		return nil, errors.New("media command input must be []byte")
	}

	args := make([]string, 0, len(argsAndInput)-1)
	for _, value := range argsAndInput[:len(argsAndInput)-1] {
		arg, ok := value.(string)
		if !ok {
			return nil, errors.New("media command argument must be string")
		}
		args = append(args, arg)
	}

	commandCtx, cancel := context.WithTimeout(ctx, mediaCommandTimeout)
	defer cancel()

	cmd := exec.CommandContext(commandCtx, name, args...)
	cmd.Stdin = bytes.NewReader(input)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if commandCtx.Err() != nil {
		return nil, commandCtx.Err()
	}
	if err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return nil, fmt.Errorf("%s failed: %s", name, detail)
	}
	return output, nil
}

func durationMilliseconds(value string) *int {
	seconds, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || seconds <= 0 {
		return nil
	}
	milliseconds := int(math.Round(seconds * 1000))
	return &milliseconds
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
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

func videoCoverObjectKey(sourceObjectKey string) string {
	dir := filepath.ToSlash(filepath.Dir(strings.TrimSpace(sourceObjectKey)))
	if dir == "." || dir == "/" {
		dir = "variants"
	}
	return strings.TrimRight(dir, "/") + "/video_cover_512.jpg"
}

func extractAIText(file repository.File, data []byte) (string, error) {
	if !isSupportedAITextFile(file) && !isLikelyUTF8Text(data) {
		return "", fmt.Errorf("unsupported AI parse source")
	}
	if !utf8.Valid(data) {
		return "", fmt.Errorf("AI parse source is not valid UTF-8 text")
	}
	text := strings.TrimSpace(strings.TrimPrefix(string(data), "\ufeff"))
	if text == "" {
		return "", fmt.Errorf("AI parse source is empty")
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return text, nil
}

func isSupportedAITextFile(file repository.File) bool {
	mimeType := strings.ToLower(strings.TrimSpace(strings.Split(file.MimeType, ";")[0]))
	if strings.HasPrefix(mimeType, "text/") {
		return true
	}
	switch mimeType {
	case "application/json", "application/xml", "application/x-ndjson", "application/javascript", "application/x-javascript", "application/sql":
		return true
	}
	extension := strings.ToLower(filepath.Ext(file.OriginalFileName))
	switch extension {
	case ".txt", ".md", ".markdown", ".json", ".jsonl", ".csv", ".tsv", ".log", ".xml", ".html", ".htm", ".css", ".js", ".ts", ".tsx", ".jsx", ".go", ".py", ".java", ".c", ".cc", ".cpp", ".h", ".hpp", ".rs", ".sql", ".yaml", ".yml", ".toml", ".ini":
		return true
	default:
		return false
	}
}

func isLikelyUTF8Text(data []byte) bool {
	if len(data) == 0 || !utf8.Valid(data) {
		return false
	}
	control := 0
	runes := 0
	for _, r := range string(data) {
		runes++
		if r < 32 && r != '\n' && r != '\r' && r != '\t' {
			control++
		}
	}
	return runes > 0 && float64(control)/float64(runes) < 0.05
}

func chunkAIText(text string) ([]string, bool) {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) == 0 {
		return []string{}, false
	}
	chunks := []string{}
	for start := 0; start < len(runes) && len(chunks) < maxAIParseChunks; {
		end := min(start+aiParseChunkRunes, len(runes))
		chunk := strings.TrimSpace(string(runes[start:end]))
		if chunk != "" {
			chunks = append(chunks, chunk)
		}
		if end >= len(runes) {
			return chunks, false
		}
		next := end - aiParseChunkOverlapRunes
		if next <= start {
			next = end
		}
		start = next
	}
	return chunks, len(chunks) >= maxAIParseChunks
}

func roughTextTokenCount(text string) int {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return 0
	}
	count := len([]rune(trimmed)) / 4
	if count < 1 {
		return 1
	}
	return count
}

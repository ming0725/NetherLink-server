package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

const fileSelectColumns = `
	id,
	owner_user_id,
	original_file_name,
	bucket,
	object_key,
	mime_type,
	size_bytes,
	etag,
	content_hash,
	storage_status,
	visibility,
	version,
	created_at,
	updated_at,
	deleted_at`

const uploadSessionSelectColumns = `
	id,
	user_id,
	file_id,
	target_type,
	status,
	upload_mode,
	s3_upload_id,
	part_size_bytes,
	total_parts,
	uploaded_parts_json,
	expected_mime_type,
	expected_size_bytes,
	expires_at,
	client_operation_id,
	created_at,
	updated_at`

const fileVariantSelectColumns = `
	id,
	file_id,
	variant_type,
	bucket,
	object_key,
	mime_type,
	size_bytes,
	width,
	height,
	duration_ms,
	etag,
	content_hash,
	created_at,
	updated_at`

const fileProcessingJobSelectColumns = `
	id,
	file_id,
	job_type,
	status,
	result_json,
	error_code,
	started_at,
	finished_at,
	created_at,
	updated_at`

type FileRepository struct {
	db DBTX
}

type File struct {
	ID               string
	OwnerUserID      *string
	OriginalFileName string
	Bucket           string
	ObjectKey        string
	MimeType         string
	SizeBytes        int64
	ETag             *string
	ContentHash      *string
	StorageStatus    string
	Visibility       string
	Version          int
	CreatedAt        time.Time
	UpdatedAt        time.Time
	DeletedAt        *time.Time
}

type FileUploadSession struct {
	ID                string
	UserID            string
	FileID            string
	TargetType        string
	Status            string
	UploadMode        string
	S3UploadID        *string
	PartSizeBytes     *int64
	TotalParts        *int
	UploadedParts     json.RawMessage
	ExpectedMimeType  *string
	ExpectedSizeBytes *int64
	ExpiresAt         time.Time
	ClientOperationID *string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type FileVariant struct {
	ID          string
	FileID      string
	VariantType string
	Bucket      string
	ObjectKey   string
	MimeType    string
	SizeBytes   int64
	Width       *int
	Height      *int
	DurationMS  *int
	ETag        *string
	ContentHash *string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type FileProcessingJob struct {
	ID         string
	FileID     string
	JobType    string
	Status     string
	Result     json.RawMessage
	ErrorCode  *string
	StartedAt  *time.Time
	FinishedAt *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type FileObjectReference struct {
	Bucket    string
	ObjectKey string
}

type FileUsage struct {
	ID          string
	FileID      string
	UsageType   string
	BusinessID  string
	OwnerUserID *string
	Visibility  string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	DeletedAt   *time.Time
}

type ExpiredUploadSessionCleanupCandidate struct {
	SessionID  string
	FileID     string
	Bucket     string
	ObjectKey  string
	UploadMode string
	S3UploadID *string
}

type UnusedFileCleanupCandidate struct {
	FileID    string
	Bucket    string
	ObjectKey string
	Variants  []FileObjectReference
}

type CreateFileParams struct {
	ID               string
	OwnerUserID      string
	OriginalFileName string
	Bucket           string
	ObjectKey        string
	MimeType         string
	SizeBytes        int64
	ContentHash      *string
	Visibility       string
}

type CreateUploadSessionParams struct {
	ID                string
	UserID            string
	FileID            string
	TargetType        string
	Status            string
	UploadMode        string
	S3UploadID        *string
	PartSizeBytes     *int64
	TotalParts        *int
	UploadedParts     json.RawMessage
	ExpectedMimeType  string
	ExpectedSizeBytes int64
	ExpiresAt         time.Time
	ClientOperationID *string
}

type CompleteUploadSessionParams struct {
	SessionID     string
	UserID        string
	UploadedParts json.RawMessage
}

type CreateFileVariantParams struct {
	FileID      string
	VariantType string
	Bucket      string
	ObjectKey   string
	MimeType    string
	SizeBytes   int64
	Width       *int
	Height      *int
	DurationMS  *int
	ETag        *string
	ContentHash *string
}

type CreateFileProcessingJobParams struct {
	FileID  string
	JobType string
	Status  string
}

type CreateFileUsageParams struct {
	FileID      string
	UsageType   string
	BusinessID  string
	OwnerUserID string
	Visibility  string
}

func NewFileRepository(db DBTX) *FileRepository {
	return &FileRepository{db: db}
}

func (r *FileRepository) Create(ctx context.Context, params CreateFileParams) (File, error) {
	visibility := normalizeText(params.Visibility)
	if visibility == "" {
		visibility = "private"
	}

	file, err := scanFile(r.db.QueryRow(ctx, `
		INSERT INTO files (
			id,
			owner_user_id,
			original_file_name,
			bucket,
			object_key,
			mime_type,
			size_bytes,
			content_hash,
			storage_status,
			visibility
		)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8, 'pending', $9)
		RETURNING `+fileSelectColumns,
		normalizeText(params.ID),
		normalizeText(params.OwnerUserID),
		normalizeText(params.OriginalFileName),
		normalizeText(params.Bucket),
		normalizeText(params.ObjectKey),
		normalizeText(params.MimeType),
		params.SizeBytes,
		normalizeTextPtr(params.ContentHash),
		visibility,
	))
	return file, translateWriteError(err)
}

func (r *FileRepository) Get(ctx context.Context, fileID string) (File, error) {
	file, err := scanFile(r.db.QueryRow(ctx, `
		SELECT `+fileSelectColumns+`
		FROM files
		WHERE id = $1::uuid
		  AND deleted_at IS NULL`,
		normalizeText(fileID),
	))
	return file, translateReadError(err)
}

func (r *FileRepository) ListDefaultAvatarFileIDs(ctx context.Context, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := r.db.Query(ctx, `
		SELECT id
		FROM files
		WHERE object_key LIKE 'defaults/avatar/%'
		  AND visibility = 'public'
		  AND storage_status = 'ready'
		  AND deleted_at IS NULL
		ORDER BY object_key ASC
		LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, translateReadError(err)
	}
	defer rows.Close()

	fileIDs := []string{}
	for rows.Next() {
		var fileID pgtype.UUID
		if err := rows.Scan(&fileID); err != nil {
			return nil, translateReadError(err)
		}
		fileIDs = append(fileIDs, uuidString(fileID))
	}
	return fileIDs, translateReadError(rows.Err())
}

func (r *FileRepository) ListDefaultGroupAvatarFileIDs(ctx context.Context, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := r.db.Query(ctx, `
		SELECT id
		FROM files
		WHERE object_key LIKE 'defaults/group-avatar/%'
		  AND visibility = 'public'
		  AND storage_status = 'ready'
		  AND deleted_at IS NULL
		ORDER BY object_key ASC
		LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, translateReadError(err)
	}
	defer rows.Close()

	fileIDs := []string{}
	for rows.Next() {
		var fileID pgtype.UUID
		if err := rows.Scan(&fileID); err != nil {
			return nil, translateReadError(err)
		}
		fileIDs = append(fileIDs, uuidString(fileID))
	}
	return fileIDs, translateReadError(rows.Err())
}

func (r *FileRepository) IsAvatarFile(ctx context.Context, fileID string) (bool, error) {
	var isAvatar bool
	err := r.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM files f
			WHERE f.id = $1::uuid
			  AND f.deleted_at IS NULL
			  AND f.visibility = 'public'
		) OR EXISTS (
			SELECT 1
			FROM file_usages fu
			JOIN files f ON f.id = fu.file_id
			WHERE fu.file_id = $1::uuid
			  AND f.deleted_at IS NULL
			  AND fu.deleted_at IS NULL
			  AND fu.usage_type IN ('avatar', 'group_avatar')
		)`,
		normalizeText(fileID),
	).Scan(&isAvatar)
	return isAvatar, translateReadError(err)
}

func (r *FileRepository) CanUserAccessFile(ctx context.Context, userID string, fileID string) (bool, error) {
	var allowed bool
	err := r.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM files f
			WHERE f.id = $2::uuid
			  AND f.deleted_at IS NULL
			  AND f.owner_user_id = $1::uuid
		) OR EXISTS (
			SELECT 1
			FROM files f
			WHERE f.id = $2::uuid
			  AND f.deleted_at IS NULL
			  AND f.visibility IN ('public', 'authenticated')
		) OR EXISTS (
			SELECT 1
			FROM file_usages fu
			JOIN files f ON f.id = fu.file_id
			WHERE fu.file_id = $2::uuid
			  AND f.deleted_at IS NULL
			  AND fu.deleted_at IS NULL
			  AND (
			    fu.visibility IN ('public', 'authenticated')
			    OR fu.owner_user_id = $1::uuid
			  )
		) OR EXISTS (
			SELECT 1
			FROM groups g
			JOIN group_members gm ON gm.group_id = g.id
			JOIN files f ON f.id = g.avatar_file_id
			WHERE g.avatar_file_id = $2::uuid
			  AND gm.user_id = $1::uuid
			  AND g.deleted_at IS NULL
			  AND f.deleted_at IS NULL
		) OR EXISTS (
			SELECT 1
			FROM message_attachments ma
			JOIN messages m ON m.id = ma.message_id
			JOIN conversations c ON c.id = m.conversation_id
			JOIN conversation_participants cp
			  ON cp.conversation_id = m.conversation_id
			 AND cp.user_id = $1::uuid
			JOIN files f ON f.id = ma.file_id
			WHERE ma.file_id = $2::uuid
			  AND c.deleted_at IS NULL
			  AND f.deleted_at IS NULL
			  AND m.recalled_at IS NULL
			  AND (cp.hidden_at IS NULL OR m.created_at > cp.hidden_at)
		) OR EXISTS (
			SELECT 1
			FROM post_media pm
			JOIN posts p ON p.id = pm.post_id
			JOIN files f ON f.id = $2::uuid
			WHERE (pm.file_id = $2::uuid OR pm.cover_file_id = $2::uuid)
			  AND p.deleted_at IS NULL
			  AND f.deleted_at IS NULL
			  AND (p.visibility = 'public' OR p.author_id = $1::uuid)
		) OR EXISTS (
			SELECT 1
			FROM ai_uploaded_files auf
			JOIN ai_conversations ac ON ac.id = auf.conversation_id
			JOIN files f ON f.id = $2::uuid
			WHERE (auf.file_id = $2::uuid OR auf.extracted_text_file_id = $2::uuid)
			  AND ac.user_id = $1::uuid
			  AND ac.deleted_at IS NULL
			  AND auf.deleted_at IS NULL
			  AND f.deleted_at IS NULL
		)`,
		normalizeText(userID),
		normalizeText(fileID),
	).Scan(&allowed)
	return allowed, translateReadError(err)
}

func (r *FileRepository) SumActiveBytesByOwner(ctx context.Context, userID string) (int64, error) {
	var bytes int64
	err := r.db.QueryRow(ctx, `
		SELECT COALESCE(SUM(size_bytes), 0)
		FROM files
		WHERE owner_user_id = $1::uuid
		  AND deleted_at IS NULL
		  AND storage_status NOT IN ('deleted', 'aborted', 'cleanup_pending')`,
		normalizeText(userID),
	).Scan(&bytes)
	return bytes, translateReadError(err)
}

func (r *FileRepository) ListVariants(ctx context.Context, fileID string) ([]FileVariant, error) {
	rows, err := r.db.Query(ctx, `
		SELECT `+fileVariantSelectColumns+`
		FROM file_variants
		WHERE file_id = $1::uuid
		ORDER BY variant_type ASC, created_at ASC`,
		normalizeText(fileID),
	)
	if err != nil {
		return nil, translateReadError(err)
	}
	defer rows.Close()

	variants := []FileVariant{}
	for rows.Next() {
		variant, err := scanFileVariant(rows)
		if err != nil {
			return nil, err
		}
		variants = append(variants, variant)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return variants, nil
}

func (r *FileRepository) MarkUploaded(ctx context.Context, fileID string, etag *string, contentHash *string, sizeBytes int64) (File, error) {
	file, err := scanFile(r.db.QueryRow(ctx, `
		UPDATE files
		SET etag = COALESCE($2, etag),
		    content_hash = COALESCE($3, content_hash),
		    size_bytes = CASE WHEN $4 > 0 THEN $4 ELSE size_bytes END,
		    storage_status = 'uploaded',
		    version = version + 1,
		    updated_at = now()
		WHERE id = $1::uuid
		  AND deleted_at IS NULL
		RETURNING `+fileSelectColumns,
		normalizeText(fileID),
		normalizeTextPtr(etag),
		normalizeTextPtr(contentHash),
		sizeBytes,
	))
	return file, translateWriteError(err)
}

func (r *FileRepository) MarkStatus(ctx context.Context, fileID string, status string) (File, error) {
	file, err := scanFile(r.db.QueryRow(ctx, `
		UPDATE files
		SET storage_status = $2,
		    version = version + 1,
		    updated_at = now()
		WHERE id = $1::uuid
		  AND deleted_at IS NULL
		RETURNING `+fileSelectColumns,
		normalizeText(fileID),
		normalizeText(status),
	))
	return file, translateWriteError(err)
}

func (r *FileRepository) CreateUploadSession(ctx context.Context, params CreateUploadSessionParams) (FileUploadSession, error) {
	status := normalizeText(params.Status)
	if status == "" {
		status = "created"
	}

	uploadedParts := params.UploadedParts
	if len(uploadedParts) == 0 {
		uploadedParts = json.RawMessage(`[]`)
	}

	session, err := scanUploadSession(r.db.QueryRow(ctx, `
		INSERT INTO file_upload_sessions (
			id,
			user_id,
			file_id,
			target_type,
			status,
			upload_mode,
			s3_upload_id,
			part_size_bytes,
			total_parts,
			uploaded_parts_json,
			expected_mime_type,
			expected_size_bytes,
			expires_at,
			client_operation_id
		)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		RETURNING `+uploadSessionSelectColumns,
		normalizeText(params.ID),
		normalizeText(params.UserID),
		normalizeText(params.FileID),
		normalizeText(params.TargetType),
		status,
		normalizeText(params.UploadMode),
		normalizeTextPtr(params.S3UploadID),
		params.PartSizeBytes,
		params.TotalParts,
		uploadedParts,
		normalizeText(params.ExpectedMimeType),
		params.ExpectedSizeBytes,
		params.ExpiresAt,
		normalizeTextPtr(params.ClientOperationID),
	))
	return session, translateWriteError(err)
}

func (r *FileRepository) GetUploadSession(ctx context.Context, userID string, sessionID string) (FileUploadSession, error) {
	session, err := scanUploadSession(r.db.QueryRow(ctx, `
		SELECT `+uploadSessionSelectColumns+`
		FROM file_upload_sessions
		WHERE id = $1::uuid
		  AND user_id = $2::uuid`,
		normalizeText(sessionID),
		normalizeText(userID),
	))
	return session, translateReadError(err)
}

func (r *FileRepository) GetUploadSessionByClientOperation(ctx context.Context, userID string, clientOperationID string) (FileUploadSession, error) {
	session, err := scanUploadSession(r.db.QueryRow(ctx, `
		SELECT `+uploadSessionSelectColumns+`
		FROM file_upload_sessions
		WHERE user_id = $1::uuid
		  AND client_operation_id = $2`,
		normalizeText(userID),
		normalizeText(clientOperationID),
	))
	return session, translateReadError(err)
}

func (r *FileRepository) CompleteUploadSession(ctx context.Context, params CompleteUploadSessionParams) (FileUploadSession, error) {
	uploadedParts := params.UploadedParts
	if len(uploadedParts) == 0 {
		uploadedParts = json.RawMessage(`[]`)
	}

	session, err := scanUploadSession(r.db.QueryRow(ctx, `
		UPDATE file_upload_sessions
		SET status = 'completed',
		    uploaded_parts_json = $3,
		    updated_at = now()
		WHERE id = $1::uuid
		  AND user_id = $2::uuid
		RETURNING `+uploadSessionSelectColumns,
		normalizeText(params.SessionID),
		normalizeText(params.UserID),
		uploadedParts,
	))
	return session, translateWriteError(err)
}

func (r *FileRepository) MarkUploadSessionStatus(ctx context.Context, userID string, sessionID string, status string) (FileUploadSession, error) {
	session, err := scanUploadSession(r.db.QueryRow(ctx, `
		UPDATE file_upload_sessions
		SET status = $3,
		    updated_at = now()
		WHERE id = $1::uuid
		  AND user_id = $2::uuid
		RETURNING `+uploadSessionSelectColumns,
		normalizeText(sessionID),
		normalizeText(userID),
		normalizeText(status),
	))
	return session, translateWriteError(err)
}

func (r *FileRepository) CreateVariant(ctx context.Context, params CreateFileVariantParams) (FileVariant, error) {
	variant, err := scanFileVariant(r.db.QueryRow(ctx, `
		INSERT INTO file_variants (
			file_id,
			variant_type,
			bucket,
			object_key,
			mime_type,
			size_bytes,
			width,
			height,
			duration_ms,
			etag,
			content_hash
		)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING `+fileVariantSelectColumns,
		normalizeText(params.FileID),
		normalizeText(params.VariantType),
		normalizeText(params.Bucket),
		normalizeText(params.ObjectKey),
		normalizeText(params.MimeType),
		params.SizeBytes,
		params.Width,
		params.Height,
		params.DurationMS,
		normalizeTextPtr(params.ETag),
		normalizeTextPtr(params.ContentHash),
	))
	return variant, translateWriteError(err)
}

func (r *FileRepository) CreateProcessingJob(ctx context.Context, params CreateFileProcessingJobParams) (FileProcessingJob, error) {
	status := normalizeText(params.Status)
	if status == "" {
		status = "pending"
	}

	job, err := scanFileProcessingJob(r.db.QueryRow(ctx, `
		INSERT INTO file_processing_jobs (
			file_id,
			job_type,
			status
		)
		VALUES ($1::uuid, $2, $3)
		RETURNING `+fileProcessingJobSelectColumns,
		normalizeText(params.FileID),
		normalizeText(params.JobType),
		status,
	))
	return job, translateWriteError(err)
}

func (r *FileRepository) ClaimNextProcessingJob(ctx context.Context) (FileProcessingJob, error) {
	job, err := scanFileProcessingJob(r.db.QueryRow(ctx, `
		UPDATE file_processing_jobs
		SET status = 'processing',
		    started_at = now(),
		    updated_at = now()
		WHERE id = (
			SELECT id
			FROM file_processing_jobs
			WHERE status = 'pending'
			ORDER BY created_at ASC
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		RETURNING `+fileProcessingJobSelectColumns,
	))
	return job, translateWriteError(err)
}

func (r *FileRepository) ResetStaleProcessingJobs(ctx context.Context, staleAfter time.Duration) (int64, error) {
	if staleAfter <= 0 {
		return 0, nil
	}

	tag, err := r.db.Exec(ctx, `
		UPDATE file_processing_jobs
		SET status = 'pending',
		    started_at = NULL,
		    updated_at = now()
		WHERE status = 'processing'
		  AND started_at IS NOT NULL
		  AND started_at < now() - ($1 * interval '1 second')`,
		staleAfter.Seconds(),
	)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (r *FileRepository) MarkProcessingJobSucceeded(ctx context.Context, jobID string, result json.RawMessage) (FileProcessingJob, error) {
	if len(result) == 0 {
		result = json.RawMessage(`{}`)
	}

	job, err := scanFileProcessingJob(r.db.QueryRow(ctx, `
		UPDATE file_processing_jobs
		SET status = 'succeeded',
		    result_json = $2,
		    error_code = NULL,
		    finished_at = now(),
		    updated_at = now()
		WHERE id = $1::uuid
		RETURNING `+fileProcessingJobSelectColumns,
		normalizeText(jobID),
		result,
	))
	return job, translateWriteError(err)
}

func (r *FileRepository) MarkProcessingJobFailed(ctx context.Context, jobID string, errorCode string, result json.RawMessage) (FileProcessingJob, error) {
	if len(result) == 0 {
		result = json.RawMessage(`{}`)
	}

	job, err := scanFileProcessingJob(r.db.QueryRow(ctx, `
		UPDATE file_processing_jobs
		SET status = 'failed',
		    result_json = $3,
		    error_code = $2,
		    finished_at = now(),
		    updated_at = now()
		WHERE id = $1::uuid
		RETURNING `+fileProcessingJobSelectColumns,
		normalizeText(jobID),
		normalizeText(errorCode),
		result,
	))
	return job, translateWriteError(err)
}

func (r *FileRepository) CreateUsage(ctx context.Context, params CreateFileUsageParams) (FileUsage, error) {
	visibility := normalizeText(params.Visibility)
	if visibility == "" {
		visibility = "private"
	}

	usage, err := scanFileUsage(r.db.QueryRow(ctx, `
		INSERT INTO file_usages (
			file_id,
			usage_type,
			business_id,
			owner_user_id,
			visibility
		)
		VALUES ($1::uuid, $2, $3::uuid, $4::uuid, $5)
		RETURNING
			id,
			file_id,
			usage_type,
			business_id,
			owner_user_id,
			visibility,
			created_at,
			updated_at,
			deleted_at`,
		normalizeText(params.FileID),
		normalizeText(params.UsageType),
		normalizeText(params.BusinessID),
		normalizeText(params.OwnerUserID),
		visibility,
	))
	return usage, translateWriteError(err)
}

func (r *FileRepository) SoftDeleteUsages(ctx context.Context, usageType string, businessID string) (int64, error) {
	tag, err := r.db.Exec(ctx, `
		UPDATE file_usages
		SET deleted_at = COALESCE(deleted_at, now()),
		    updated_at = now()
		WHERE usage_type = $1
		  AND business_id = $2::uuid
		  AND deleted_at IS NULL`,
		normalizeText(usageType),
		normalizeText(businessID),
	)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (r *FileRepository) HasActiveReferences(ctx context.Context, fileID string) (bool, error) {
	var referenced bool
	err := r.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM file_usages fu
			WHERE fu.file_id = $1::uuid
			  AND fu.deleted_at IS NULL
		) OR EXISTS (
			SELECT 1
			FROM users u
			WHERE u.avatar_file_id = $1::uuid
			  AND u.deleted_at IS NULL
		) OR EXISTS (
			SELECT 1
			FROM groups g
			WHERE g.avatar_file_id = $1::uuid
			  AND g.deleted_at IS NULL
		) OR EXISTS (
			SELECT 1
			FROM message_attachments ma
			WHERE ma.file_id = $1::uuid
		) OR EXISTS (
			SELECT 1
			FROM post_media pm
			WHERE pm.file_id = $1::uuid
			   OR pm.cover_file_id = $1::uuid
		) OR EXISTS (
			SELECT 1
			FROM ai_uploaded_files auf
			WHERE (auf.file_id = $1::uuid OR auf.extracted_text_file_id = $1::uuid)
			  AND auf.deleted_at IS NULL
		)`,
		normalizeText(fileID),
	).Scan(&referenced)
	return referenced, translateReadError(err)
}

func (r *FileRepository) CanDeleteAIParseSourceAfterParse(ctx context.Context, fileID string) (bool, error) {
	var allowed bool
	err := r.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM files f
			WHERE f.id = $1::uuid
			  AND f.deleted_at IS NULL
		)
		AND NOT EXISTS (
			SELECT 1
			FROM file_usages fu
			WHERE fu.file_id = $1::uuid
			  AND fu.deleted_at IS NULL
			  AND fu.usage_type <> 'ai_upload'
		)
		AND NOT EXISTS (
			SELECT 1
			FROM users u
			WHERE u.avatar_file_id = $1::uuid
			  AND u.deleted_at IS NULL
		)
		AND NOT EXISTS (
			SELECT 1
			FROM groups g
			WHERE g.avatar_file_id = $1::uuid
			  AND g.deleted_at IS NULL
		)
		AND NOT EXISTS (
			SELECT 1
			FROM message_attachments ma
			WHERE ma.file_id = $1::uuid
		)
		AND NOT EXISTS (
			SELECT 1
			FROM post_media pm
			WHERE pm.file_id = $1::uuid
			   OR pm.cover_file_id = $1::uuid
		)
		AND NOT EXISTS (
			SELECT 1
			FROM ai_uploaded_files auf
			WHERE auf.file_id = $1::uuid
			  AND auf.deleted_at IS NULL
			  AND COALESCE(NULLIF(auf.retention_policy, ''), 'conversation') <> 'source_deleted_after_parse'
		)`,
		normalizeText(fileID),
	).Scan(&allowed)
	return allowed, translateReadError(err)
}

func (r *FileRepository) SoftDelete(ctx context.Context, fileID string) error {
	tag, err := r.db.Exec(ctx, `
		UPDATE files
		SET storage_status = 'deleted',
		    deleted_at = COALESCE(deleted_at, now()),
		    version = version + 1,
		    updated_at = now()
		WHERE id = $1::uuid
		  AND deleted_at IS NULL`,
		normalizeText(fileID),
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *FileRepository) ClaimExpiredUploadSessionsForCleanup(ctx context.Context, limit int, retryAfter time.Duration) ([]ExpiredUploadSessionCleanupCandidate, error) {
	if limit <= 0 {
		limit = 25
	}
	if retryAfter <= 0 {
		retryAfter = 15 * time.Minute
	}

	rows, err := r.db.Query(ctx, `
		WITH candidates AS (
			SELECT fus.id
			FROM file_upload_sessions fus
			JOIN files f ON f.id = fus.file_id
			WHERE f.deleted_at IS NULL
			  AND (
			    (fus.status = 'created' AND fus.expires_at < now())
			    OR (fus.status = 'cleanup_pending' AND fus.updated_at < now() - ($2 * interval '1 second'))
			  )
			ORDER BY fus.expires_at ASC, fus.created_at ASC
			FOR UPDATE SKIP LOCKED
			LIMIT $1
		),
		marked AS (
			UPDATE file_upload_sessions fus
			SET status = 'cleanup_pending',
			    updated_at = now()
			FROM candidates
			WHERE fus.id = candidates.id
			RETURNING fus.id, fus.file_id, fus.upload_mode, fus.s3_upload_id
		)
		SELECT marked.id, marked.file_id, marked.upload_mode, marked.s3_upload_id, f.bucket, f.object_key
		FROM marked
		JOIN files f ON f.id = marked.file_id
		ORDER BY marked.id`,
		limit,
		retryAfter.Seconds(),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	candidates := []ExpiredUploadSessionCleanupCandidate{}
	for rows.Next() {
		var (
			candidate  ExpiredUploadSessionCleanupCandidate
			sessionID  pgtype.UUID
			fileID     pgtype.UUID
			s3UploadID sql.NullString
		)
		if err := rows.Scan(
			&sessionID,
			&fileID,
			&candidate.UploadMode,
			&s3UploadID,
			&candidate.Bucket,
			&candidate.ObjectKey,
		); err != nil {
			return nil, err
		}
		candidate.SessionID = uuidString(sessionID)
		candidate.FileID = uuidString(fileID)
		candidate.S3UploadID = stringPtrFromNull(s3UploadID)
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return candidates, nil
}

func (r *FileRepository) MarkExpiredUploadSessionCleaned(ctx context.Context, sessionID string, fileID string) error {
	tag, err := r.db.Exec(ctx, `
		WITH session_update AS (
			UPDATE file_upload_sessions
			SET status = 'aborted',
			    updated_at = now()
			WHERE id = $1::uuid
			  AND file_id = $2::uuid
			  AND status = 'cleanup_pending'
			RETURNING file_id
		)
		UPDATE files f
		SET storage_status = 'deleted',
		    deleted_at = COALESCE(f.deleted_at, now()),
		    version = version + 1,
		    updated_at = now()
		FROM session_update
		WHERE f.id = session_update.file_id`,
		normalizeText(sessionID),
		normalizeText(fileID),
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *FileRepository) ClaimUnusedFilesForCleanup(ctx context.Context, retention time.Duration, limit int, retryAfter time.Duration) ([]UnusedFileCleanupCandidate, error) {
	if retention <= 0 {
		retention = 24 * time.Hour
	}
	if limit <= 0 {
		limit = 25
	}
	if retryAfter <= 0 {
		retryAfter = 15 * time.Minute
	}

	rows, err := r.db.Query(ctx, `
		WITH candidates AS (
			SELECT f.id
			FROM files f
			WHERE f.deleted_at IS NULL
			  AND (
			    (
			      f.storage_status IN ('ready', 'failed', 'uploaded', 'aborted')
			      AND f.updated_at < now() - ($1 * interval '1 second')
			    )
			    OR (
			      f.storage_status = 'cleanup_pending'
			      AND f.updated_at < now() - ($3 * interval '1 second')
			    )
			  )
			  AND NOT EXISTS (
			    SELECT 1 FROM file_upload_sessions fus
			    WHERE fus.file_id = f.id
			      AND fus.status IN ('created', 'cleanup_pending')
			  )
			  AND NOT EXISTS (
			    SELECT 1 FROM file_usages fu
			    WHERE fu.file_id = f.id
			      AND fu.deleted_at IS NULL
			  )
			  AND NOT EXISTS (
			    SELECT 1 FROM file_usages fu
			    WHERE fu.file_id = f.id
			      AND fu.deleted_at IS NOT NULL
			      AND fu.deleted_at > now() - ($1 * interval '1 second')
			  )
			  AND NOT EXISTS (
			    SELECT 1 FROM users u
			    WHERE u.avatar_file_id = f.id
			      AND u.deleted_at IS NULL
			  )
			  AND NOT EXISTS (
			    SELECT 1 FROM groups g
			    WHERE g.avatar_file_id = f.id
			      AND g.deleted_at IS NULL
			  )
			  AND NOT EXISTS (
			    SELECT 1 FROM message_attachments ma
			    WHERE ma.file_id = f.id
			  )
			  AND NOT EXISTS (
			    SELECT 1 FROM post_media pm
			    WHERE pm.file_id = f.id
			       OR pm.cover_file_id = f.id
			  )
			  AND NOT EXISTS (
			    SELECT 1 FROM ai_uploaded_files auf
			    WHERE (auf.file_id = f.id OR auf.extracted_text_file_id = f.id)
			      AND auf.deleted_at IS NULL
			  )
			ORDER BY f.updated_at ASC
			FOR UPDATE SKIP LOCKED
			LIMIT $2
		),
		marked AS (
			UPDATE files f
			SET storage_status = 'cleanup_pending',
			    updated_at = now()
			FROM candidates
			WHERE f.id = candidates.id
			RETURNING f.id, f.bucket, f.object_key
		)
		SELECT marked.id, marked.bucket, marked.object_key, fv.bucket, fv.object_key
		FROM marked
		LEFT JOIN file_variants fv ON fv.file_id = marked.id
		ORDER BY marked.id`,
		retention.Seconds(),
		limit,
		retryAfter.Seconds(),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	candidatesByID := map[string]*UnusedFileCleanupCandidate{}
	order := []string{}
	for rows.Next() {
		var (
			fileID        pgtype.UUID
			variantBucket sql.NullString
			variantKey    sql.NullString
			bucket        string
			objectKey     string
		)
		if err := rows.Scan(&fileID, &bucket, &objectKey, &variantBucket, &variantKey); err != nil {
			return nil, err
		}

		id := uuidString(fileID)
		candidate := candidatesByID[id]
		if candidate == nil {
			candidate = &UnusedFileCleanupCandidate{
				FileID:    id,
				Bucket:    bucket,
				ObjectKey: objectKey,
			}
			candidatesByID[id] = candidate
			order = append(order, id)
		}
		if variantBucket.Valid && variantKey.Valid {
			candidate.Variants = append(candidate.Variants, FileObjectReference{
				Bucket:    variantBucket.String,
				ObjectKey: variantKey.String,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	candidates := make([]UnusedFileCleanupCandidate, 0, len(order))
	for _, id := range order {
		candidates = append(candidates, *candidatesByID[id])
	}
	return candidates, nil
}

func (r *FileRepository) MarkUnusedFileCleaned(ctx context.Context, fileID string) error {
	tag, err := r.db.Exec(ctx, `
		WITH cleaned_variants AS (
			DELETE FROM file_variants
			WHERE file_id = $1::uuid
		)
		UPDATE files
		SET storage_status = 'deleted',
		    deleted_at = COALESCE(deleted_at, now()),
		    version = version + 1,
		    updated_at = now()
		WHERE id = $1::uuid
		  AND storage_status = 'cleanup_pending'
		  AND deleted_at IS NULL`,
		normalizeText(fileID),
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func scanFile(row scanner) (File, error) {
	var (
		file        File
		id          pgtype.UUID
		ownerUserID pgtype.UUID
		etag        sql.NullString
		contentHash sql.NullString
		deletedAt   sql.NullTime
	)

	err := row.Scan(
		&id,
		&ownerUserID,
		&file.OriginalFileName,
		&file.Bucket,
		&file.ObjectKey,
		&file.MimeType,
		&file.SizeBytes,
		&etag,
		&contentHash,
		&file.StorageStatus,
		&file.Visibility,
		&file.Version,
		&file.CreatedAt,
		&file.UpdatedAt,
		&deletedAt,
	)
	if err != nil {
		return File{}, err
	}

	file.ID = uuidString(id)
	file.OwnerUserID = uuidStringPtr(ownerUserID)
	file.ETag = stringPtrFromNull(etag)
	file.ContentHash = stringPtrFromNull(contentHash)
	if deletedAt.Valid {
		file.DeletedAt = &deletedAt.Time
	}
	return file, nil
}

func scanUploadSession(row scanner) (FileUploadSession, error) {
	var (
		session           FileUploadSession
		id                pgtype.UUID
		userID            pgtype.UUID
		fileID            pgtype.UUID
		s3UploadID        sql.NullString
		partSizeBytes     sql.NullInt64
		totalParts        sql.NullInt64
		expectedMimeType  sql.NullString
		expectedSizeBytes sql.NullInt64
		clientOperationID sql.NullString
	)

	err := row.Scan(
		&id,
		&userID,
		&fileID,
		&session.TargetType,
		&session.Status,
		&session.UploadMode,
		&s3UploadID,
		&partSizeBytes,
		&totalParts,
		&session.UploadedParts,
		&expectedMimeType,
		&expectedSizeBytes,
		&session.ExpiresAt,
		&clientOperationID,
		&session.CreatedAt,
		&session.UpdatedAt,
	)
	if err != nil {
		return FileUploadSession{}, err
	}

	session.ID = uuidString(id)
	session.UserID = uuidString(userID)
	session.FileID = uuidString(fileID)
	session.S3UploadID = stringPtrFromNull(s3UploadID)
	if partSizeBytes.Valid {
		value := partSizeBytes.Int64
		session.PartSizeBytes = &value
	}
	if totalParts.Valid {
		value := int(totalParts.Int64)
		session.TotalParts = &value
	}
	session.ExpectedMimeType = stringPtrFromNull(expectedMimeType)
	if expectedSizeBytes.Valid {
		value := expectedSizeBytes.Int64
		session.ExpectedSizeBytes = &value
	}
	session.ClientOperationID = stringPtrFromNull(clientOperationID)
	session.UploadedParts = jsonOrDefaultArray(session.UploadedParts)
	return session, nil
}

func scanFileVariant(row scanner) (FileVariant, error) {
	var (
		variant     FileVariant
		id          pgtype.UUID
		fileID      pgtype.UUID
		width       sql.NullInt64
		height      sql.NullInt64
		durationMS  sql.NullInt64
		etag        sql.NullString
		contentHash sql.NullString
	)

	err := row.Scan(
		&id,
		&fileID,
		&variant.VariantType,
		&variant.Bucket,
		&variant.ObjectKey,
		&variant.MimeType,
		&variant.SizeBytes,
		&width,
		&height,
		&durationMS,
		&etag,
		&contentHash,
		&variant.CreatedAt,
		&variant.UpdatedAt,
	)
	if err != nil {
		return FileVariant{}, err
	}

	variant.ID = uuidString(id)
	variant.FileID = uuidString(fileID)
	if width.Valid {
		value := int(width.Int64)
		variant.Width = &value
	}
	if height.Valid {
		value := int(height.Int64)
		variant.Height = &value
	}
	if durationMS.Valid {
		value := int(durationMS.Int64)
		variant.DurationMS = &value
	}
	variant.ETag = stringPtrFromNull(etag)
	variant.ContentHash = stringPtrFromNull(contentHash)
	return variant, nil
}

func scanFileUsage(row scanner) (FileUsage, error) {
	var (
		usage       FileUsage
		id          pgtype.UUID
		fileID      pgtype.UUID
		businessID  pgtype.UUID
		ownerUserID pgtype.UUID
		deletedAt   sql.NullTime
	)

	err := row.Scan(
		&id,
		&fileID,
		&usage.UsageType,
		&businessID,
		&ownerUserID,
		&usage.Visibility,
		&usage.CreatedAt,
		&usage.UpdatedAt,
		&deletedAt,
	)
	if err != nil {
		return FileUsage{}, err
	}

	usage.ID = uuidString(id)
	usage.FileID = uuidString(fileID)
	usage.BusinessID = uuidString(businessID)
	usage.OwnerUserID = uuidStringPtr(ownerUserID)
	if deletedAt.Valid {
		usage.DeletedAt = &deletedAt.Time
	}
	return usage, nil
}

func scanFileProcessingJob(row scanner) (FileProcessingJob, error) {
	var (
		job        FileProcessingJob
		id         pgtype.UUID
		fileID     pgtype.UUID
		errorCode  sql.NullString
		startedAt  sql.NullTime
		finishedAt sql.NullTime
	)

	err := row.Scan(
		&id,
		&fileID,
		&job.JobType,
		&job.Status,
		&job.Result,
		&errorCode,
		&startedAt,
		&finishedAt,
		&job.CreatedAt,
		&job.UpdatedAt,
	)
	if err != nil {
		return FileProcessingJob{}, err
	}

	job.ID = uuidString(id)
	job.FileID = uuidString(fileID)
	job.Result = jsonOrDefault(job.Result)
	job.ErrorCode = stringPtrFromNull(errorCode)
	if startedAt.Valid {
		job.StartedAt = &startedAt.Time
	}
	if finishedAt.Valid {
		job.FinishedAt = &finishedAt.Time
	}
	return job, nil
}

func stringPtrFromNull(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func jsonOrDefaultArray(value json.RawMessage) json.RawMessage {
	if len(value) == 0 {
		return json.RawMessage(`[]`)
	}
	return value
}

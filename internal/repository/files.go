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

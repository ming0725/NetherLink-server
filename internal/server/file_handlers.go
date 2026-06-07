package server

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	filesvc "nlserver/internal/files"
	"nlserver/internal/repository"
	"nlserver/internal/storage"
)

type createUploadSessionRequest struct {
	TargetType        string  `json:"targetType"`
	FileName          string  `json:"fileName"`
	MimeType          string  `json:"mimeType"`
	SizeBytes         int64   `json:"sizeBytes"`
	ContentHash       *string `json:"contentHash"`
	UploadMode        string  `json:"uploadMode"`
	ClientOperationID *string `json:"clientOperationId"`
}

type uploadSessionResponse struct {
	UploadSessionID string            `json:"uploadSessionId"`
	FileID          string            `json:"fileId"`
	UploadMode      string            `json:"uploadMode"`
	PartSizeBytes   *int64            `json:"partSizeBytes,omitempty"`
	TotalParts      *int              `json:"totalParts,omitempty"`
	UploadURL       *string           `json:"uploadUrl,omitempty"`
	Headers         map[string]string `json:"headers"`
	ExpiresAt       time.Time         `json:"expiresAt"`
}

type uploadSessionStatusResponse struct {
	Status            string                 `json:"status"`
	UploadMode        string                 `json:"uploadMode"`
	PartSizeBytes     *int64                 `json:"partSizeBytes,omitempty"`
	TotalParts        *int                   `json:"totalParts,omitempty"`
	UploadedParts     []filesvc.UploadedPart `json:"uploadedParts"`
	ExpiresAt         time.Time              `json:"expiresAt"`
	FileID            string                 `json:"fileId"`
	TargetType        string                 `json:"targetType"`
	ExpectedMimeType  *string                `json:"expectedMimeType,omitempty"`
	ExpectedSizeBytes *int64                 `json:"expectedSizeBytes,omitempty"`
}

type presignUploadPartsRequest struct {
	PartNumbers []int `json:"partNumbers"`
}

type presignUploadPartsResponse struct {
	Parts []filesvc.PresignedPart `json:"parts"`
}

type completeUploadSessionRequest struct {
	ETag        *string                 `json:"etag"`
	Parts       []filesvc.CompletedPart `json:"parts"`
	ContentHash *string                 `json:"contentHash"`
}

type completeUploadSessionResponse struct {
	FileID           string  `json:"fileId"`
	URL              string  `json:"url"`
	ETag             *string `json:"etag"`
	ContentHash      *string `json:"contentHash"`
	Version          int     `json:"version"`
	ProcessingStatus string  `json:"processingStatus"`
}

type fileResponse struct {
	FileID           string                `json:"fileId"`
	FileName         string                `json:"fileName"`
	MimeType         string                `json:"mimeType"`
	SizeBytes        int64                 `json:"sizeBytes"`
	URL              string                `json:"url"`
	Visibility       string                `json:"visibility"`
	ProcessingStatus string                `json:"processingStatus"`
	Version          int                   `json:"version"`
	ETag             *string               `json:"etag"`
	ContentHash      *string               `json:"contentHash"`
	CreatedAt        time.Time             `json:"createdAt"`
	UpdatedAt        time.Time             `json:"updatedAt"`
	Variants         []fileVariantResponse `json:"variants"`
}

type fileVariantResponse struct {
	VariantID   string    `json:"variantId"`
	VariantType string    `json:"variantType"`
	URL         string    `json:"url"`
	MimeType    string    `json:"mimeType"`
	SizeBytes   int64     `json:"sizeBytes"`
	Width       *int      `json:"width,omitempty"`
	Height      *int      `json:"height,omitempty"`
	DurationMS  *int      `json:"durationMs,omitempty"`
	ETag        *string   `json:"etag"`
	ContentHash *string   `json:"contentHash"`
	CreatedAt   time.Time `json:"createdAt"`
}

type downloadURLResponse struct {
	URL       string            `json:"url"`
	ExpiresAt time.Time         `json:"expiresAt"`
	Headers   map[string]string `json:"headers,omitempty"`
}

type avatarUploadResponse struct {
	AvatarURL         string  `json:"avatarUrl"`
	AvatarVersion     int     `json:"avatarVersion"`
	AvatarEtag        *string `json:"avatarEtag"`
	AvatarContentHash *string `json:"avatarContentHash"`
	FileID            string  `json:"fileId"`
}

type okResponse struct {
	OK bool `json:"ok"`
}

func (s *Server) handleFileRoutes(w http.ResponseWriter, r *http.Request) {
	const prefix = "/api/v1/files/"
	tail := strings.Trim(strings.TrimPrefix(r.URL.Path, prefix), "/")
	parts := []string{}
	if tail != "" {
		parts = strings.Split(tail, "/")
	}

	switch {
	case r.Method == http.MethodPost && len(parts) == 1 && parts[0] == "upload-session":
		s.handleCreateUploadSession(w, r)
	case r.Method == http.MethodGet && len(parts) == 2 && parts[0] == "upload-session":
		r.SetPathValue("uploadSessionId", parts[1])
		s.handleGetUploadSession(w, r)
	case r.Method == http.MethodPost && len(parts) == 3 && parts[0] == "upload-session" && parts[2] == "parts":
		r.SetPathValue("uploadSessionId", parts[1])
		s.handlePresignUploadParts(w, r)
	case r.Method == http.MethodPost && len(parts) == 3 && parts[0] == "upload-session" && parts[2] == "complete":
		r.SetPathValue("uploadSessionId", parts[1])
		s.handleCompleteUploadSession(w, r)
	case r.Method == http.MethodPost && len(parts) == 3 && parts[0] == "upload-session" && parts[2] == "abort":
		r.SetPathValue("uploadSessionId", parts[1])
		s.handleAbortUploadSession(w, r)
	case r.Method == http.MethodGet && len(parts) == 1:
		r.SetPathValue("fileId", parts[0])
		s.handleGetFile(w, r)
	case r.Method == http.MethodGet && len(parts) == 2 && parts[1] == "download-url":
		r.SetPathValue("fileId", parts[0])
		s.handleGetFileDownloadURL(w, r)
	case r.Method == http.MethodDelete && len(parts) == 1:
		r.SetPathValue("fileId", parts[0])
		s.handleDeleteFile(w, r)
	default:
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Resource not found")
	}
}

func (s *Server) handleCreateUploadSession(w http.ResponseWriter, r *http.Request) {
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	if s.fileService == nil {
		writeError(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "File service is not configured")
		return
	}

	var req createUploadSessionRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeRequestError(w, r, err)
		return
	}

	result, err := s.fileService.CreateUploadSession(r.Context(), filesvc.CreateUploadSessionRequest{
		UserID:            principal.UserID,
		TargetType:        req.TargetType,
		FileName:          req.FileName,
		MimeType:          req.MimeType,
		SizeBytes:         req.SizeBytes,
		ContentHash:       req.ContentHash,
		UploadMode:        req.UploadMode,
		ClientOperationID: req.ClientOperationID,
	})
	if err != nil {
		writeFileError(w, r, err)
		return
	}

	writeJSON(w, http.StatusCreated, uploadSessionResponse{
		UploadSessionID: result.UploadSessionID,
		FileID:          result.FileID,
		UploadMode:      result.UploadMode,
		PartSizeBytes:   result.PartSizeBytes,
		TotalParts:      result.TotalParts,
		UploadURL:       result.UploadURL,
		Headers:         result.Headers,
		ExpiresAt:       result.ExpiresAt,
	})
}

func (s *Server) handleGetUploadSession(w http.ResponseWriter, r *http.Request) {
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	if s.fileService == nil {
		writeError(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "File service is not configured")
		return
	}

	sessionID := r.PathValue("uploadSessionId")
	if err := validateUUID(sessionID, "uploadSessionId"); err != nil {
		writeRequestError(w, r, err)
		return
	}

	status, err := s.fileService.GetUploadSession(r.Context(), principal.UserID, sessionID)
	if err != nil {
		writeFileError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, uploadSessionStatusResponse{
		Status:            status.Status,
		UploadMode:        status.UploadMode,
		PartSizeBytes:     status.PartSizeBytes,
		TotalParts:        status.TotalParts,
		UploadedParts:     status.UploadedParts,
		ExpiresAt:         status.ExpiresAt,
		FileID:            status.FileID,
		TargetType:        status.TargetType,
		ExpectedMimeType:  status.ExpectedMimeType,
		ExpectedSizeBytes: status.ExpectedSizeBytes,
	})
}

func (s *Server) handlePresignUploadParts(w http.ResponseWriter, r *http.Request) {
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	if s.fileService == nil {
		writeError(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "File service is not configured")
		return
	}

	sessionID := r.PathValue("uploadSessionId")
	if err := validateUUID(sessionID, "uploadSessionId"); err != nil {
		writeRequestError(w, r, err)
		return
	}

	var req presignUploadPartsRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeRequestError(w, r, err)
		return
	}

	parts, err := s.fileService.PresignUploadParts(r.Context(), filesvc.PresignPartsRequest{
		UserID:      principal.UserID,
		SessionID:   sessionID,
		PartNumbers: req.PartNumbers,
	})
	if err != nil {
		writeFileError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, presignUploadPartsResponse{Parts: parts})
}

func (s *Server) handleCompleteUploadSession(w http.ResponseWriter, r *http.Request) {
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	if s.fileService == nil {
		writeError(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "File service is not configured")
		return
	}

	sessionID := r.PathValue("uploadSessionId")
	if err := validateUUID(sessionID, "uploadSessionId"); err != nil {
		writeRequestError(w, r, err)
		return
	}

	var req completeUploadSessionRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeRequestError(w, r, err)
		return
	}

	result, err := s.fileService.CompleteUploadSession(r.Context(), filesvc.CompleteUploadSessionRequest{
		UserID:      principal.UserID,
		SessionID:   sessionID,
		ETag:        req.ETag,
		Parts:       req.Parts,
		ContentHash: req.ContentHash,
	})
	if err != nil {
		writeFileError(w, r, err)
		return
	}

	response := completeUploadSessionResponse{
		FileID:           result.FileID,
		URL:              result.URL,
		ETag:             result.ETag,
		ContentHash:      result.ContentHash,
		Version:          result.Version,
		ProcessingStatus: result.ProcessingStatus,
	}
	writeJSONWithETag(w, r, http.StatusOK, resourceETag("file", result.FileID, result.Version), response)
}

func (s *Server) handleAbortUploadSession(w http.ResponseWriter, r *http.Request) {
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	if s.fileService == nil {
		writeError(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "File service is not configured")
		return
	}

	sessionID := r.PathValue("uploadSessionId")
	if err := validateUUID(sessionID, "uploadSessionId"); err != nil {
		writeRequestError(w, r, err)
		return
	}

	if err := s.fileService.AbortUploadSession(r.Context(), principal.UserID, sessionID); err != nil {
		writeFileError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, okResponse{OK: true})
}

func (s *Server) handleUploadFile(w http.ResponseWriter, r *http.Request) {
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	if s.fileService == nil {
		writeError(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "File service is not configured")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "file is required")
		return
	}
	defer file.Close()

	targetType := strings.TrimSpace(r.FormValue("targetType"))
	if targetType == "" {
		writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "targetType is required")
		return
	}

	mimeType := strings.TrimSpace(header.Header.Get("Content-Type"))
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	var contentHash *string
	if value := strings.TrimSpace(r.FormValue("contentHash")); value != "" {
		contentHash = &value
	}

	result, err := s.fileService.StoreFile(r.Context(), filesvc.StoreFileRequest{
		UserID:      principal.UserID,
		TargetType:  targetType,
		FileName:    header.Filename,
		MimeType:    mimeType,
		SizeBytes:   header.Size,
		ContentHash: contentHash,
		Body:        file,
	})
	if err != nil {
		writeFileError(w, r, err)
		return
	}
	response := makeFileResponse(result)
	writeJSONWithETag(w, r, http.StatusCreated, resourceETag("file", result.FileID, result.Version), response)
}

func (s *Server) handleUploadMeAvatar(w http.ResponseWriter, r *http.Request) {
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	if s.fileService == nil {
		writeError(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "File service is not configured")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "file is required")
		return
	}
	defer file.Close()

	mimeType := strings.TrimSpace(header.Header.Get("Content-Type"))
	if !strings.HasPrefix(strings.ToLower(mimeType), "image/") {
		writeError(w, r, http.StatusUnsupportedMediaType, "FILE_TYPE_NOT_ALLOWED", "Avatar must be an image")
		return
	}
	var contentHash *string
	if value := strings.TrimSpace(r.FormValue("contentHash")); value != "" {
		contentHash = &value
	}
	expectedVersion, reqErr := expectedVersionFromQuery(r)
	if reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}
	if expectedVersion != nil {
		current, err := s.repos.Users.GetByID(r.Context(), principal.UserID)
		if err != nil {
			writeRepositoryError(w, r, err)
			return
		}
		if !checkResourceVersion(w, r, resourceETag("user", current.ID, current.Version), current.Version, expectedVersion) {
			return
		}
	}

	result, err := s.fileService.UploadAvatar(r.Context(), filesvc.StoreFileRequest{
		UserID:      principal.UserID,
		FileName:    header.Filename,
		MimeType:    mimeType,
		SizeBytes:   header.Size,
		ContentHash: contentHash,
		Body:        file,
	})
	if err != nil {
		writeFileError(w, r, err)
		return
	}

	response := avatarUploadResponse{
		AvatarURL:         result.AvatarURL,
		AvatarVersion:     result.AvatarVersion,
		AvatarEtag:        result.AvatarEtag,
		AvatarContentHash: result.AvatarContentHash,
		FileID:            result.FileID,
	}
	user, err := s.repos.Users.GetByID(r.Context(), principal.UserID)
	if err != nil {
		writeRepositoryError(w, r, err)
		return
	}
	s.publishProfileRealtimeEvent(r.Context(), principal.UserID, makeAvatarUpdatedPayload("avatar.updated", makePublicUserResponse(user), response))
	writeJSONWithETag(w, r, http.StatusCreated, resourceETag("user", principal.UserID, result.AvatarVersion), response)
}

func (s *Server) handleGetPublicAvatarFile(w http.ResponseWriter, r *http.Request) {
	if s.repos.Files == nil || s.storage == nil {
		writeError(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "File service is not configured")
		return
	}

	fileID := r.PathValue("fileId")
	if err := validateUUID(fileID, "fileId"); err != nil {
		writeRequestError(w, r, err)
		return
	}

	file, err := s.repos.Files.Get(r.Context(), fileID)
	if err != nil {
		writeRepositoryError(w, r, err)
		return
	}
	isAvatar, err := s.repos.Files.IsAvatarFile(r.Context(), file.ID)
	if err != nil {
		writeRepositoryError(w, r, err)
		return
	}
	if !isAvatar {
		writeError(w, r, http.StatusForbidden, "FORBIDDEN", "Forbidden")
		return
	}

	target := avatarFileTarget{
		bucket:       file.Bucket,
		objectKey:    file.ObjectKey,
		mimeType:     file.MimeType,
		sizeBytes:    file.SizeBytes,
		etag:         file.ETag,
		lastModified: file.UpdatedAt,
		cacheID:      file.ID,
	}
	if variant := normalizeAvatarVariant(r.URL.Query().Get("variant")); variant != "" && variant != "original" {
		variants, err := s.repos.Files.ListVariants(r.Context(), file.ID)
		if err != nil {
			writeRepositoryError(w, r, err)
			return
		}
		found := false
		for _, candidate := range variants {
			if candidate.VariantType != variant {
				continue
			}
			target = avatarFileTarget{
				bucket:       candidate.Bucket,
				objectKey:    candidate.ObjectKey,
				mimeType:     candidate.MimeType,
				sizeBytes:    candidate.SizeBytes,
				etag:         candidate.ETag,
				lastModified: candidate.UpdatedAt,
				cacheID:      candidate.ID,
			}
			found = true
			break
		}
		if !found && variant != "thumbnail_512" {
			writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Resource not found")
			return
		}
	}

	etag := avatarFileETag(target, file.Version)
	setAvatarFileHeaders(w, target, etag)
	if ifNoneMatch(r, etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	object, err := s.storage.GetObject(r.Context(), storage.GetObjectInput{
		Bucket:    target.bucket,
		ObjectKey: target.objectKey,
	})
	if err != nil {
		clearAvatarFileHeaders(w)
		writeFileError(w, r, err)
		return
	}
	defer object.Body.Close()

	if target.mimeType == "" {
		target.mimeType = object.Info.ContentType
	}
	if target.sizeBytes <= 0 {
		target.sizeBytes = object.Info.SizeBytes
	}
	if target.etag == nil && strings.TrimSpace(object.Info.ETag) != "" {
		target.etag = &object.Info.ETag
		etag = avatarFileETag(target, file.Version)
	}
	if target.lastModified.IsZero() {
		target.lastModified = object.Info.LastModified
	}
	setAvatarFileHeaders(w, target, etag)
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, object.Body); err != nil && s.logger != nil {
		s.logger.Warn("failed to stream avatar file", "file_id", file.ID, "error", err)
	}
}

func (s *Server) handleGetFile(w http.ResponseWriter, r *http.Request) {
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	if s.fileService == nil {
		writeError(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "File service is not configured")
		return
	}
	fileID := r.PathValue("fileId")
	if err := validateUUID(fileID, "fileId"); err != nil {
		writeRequestError(w, r, err)
		return
	}

	result, err := s.fileService.GetFile(r.Context(), principal.UserID, fileID)
	if err != nil {
		writeFileError(w, r, err)
		return
	}
	response := makeFileResponse(result)
	writeJSONWithETag(w, r, http.StatusOK, resourceETag("file", result.FileID, result.Version), response)
}

func (s *Server) handleGetFileDownloadURL(w http.ResponseWriter, r *http.Request) {
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	if s.fileService == nil {
		writeError(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "File service is not configured")
		return
	}
	fileID := r.PathValue("fileId")
	if err := validateUUID(fileID, "fileId"); err != nil {
		writeRequestError(w, r, err)
		return
	}

	result, err := s.fileService.PresignDownloadURL(r.Context(), principal.UserID, fileID, r.URL.Query().Get("variant"))
	if err != nil {
		writeFileError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, downloadURLResponse{
		URL:       result.URL,
		ExpiresAt: result.ExpiresAt,
		Headers:   result.Headers,
	})
}

func (s *Server) handleDeleteFile(w http.ResponseWriter, r *http.Request) {
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	if s.fileService == nil {
		writeError(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "File service is not configured")
		return
	}
	fileID := r.PathValue("fileId")
	if err := validateUUID(fileID, "fileId"); err != nil {
		writeRequestError(w, r, err)
		return
	}
	expectedVersion, reqErr := expectedVersionFromQuery(r)
	if reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}
	current, err := s.fileService.GetFile(r.Context(), principal.UserID, fileID)
	if err != nil {
		writeFileError(w, r, err)
		return
	}
	if !checkResourceVersion(w, r, resourceETag("file", current.FileID, current.Version), current.Version, expectedVersion) {
		return
	}

	if err := s.fileService.DeleteFile(r.Context(), principal.UserID, fileID); err != nil {
		writeFileError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, okResponse{OK: true})
}

func makeFileResponse(result filesvc.FileResult) fileResponse {
	variants := make([]fileVariantResponse, 0, len(result.Variants))
	for _, variant := range result.Variants {
		variants = append(variants, fileVariantResponse{
			VariantID:   variant.VariantID,
			VariantType: variant.VariantType,
			URL:         variant.URL,
			MimeType:    variant.MimeType,
			SizeBytes:   variant.SizeBytes,
			Width:       variant.Width,
			Height:      variant.Height,
			DurationMS:  variant.DurationMS,
			ETag:        variant.ETag,
			ContentHash: variant.ContentHash,
			CreatedAt:   variant.CreatedAt,
		})
	}
	return fileResponse{
		FileID:           result.FileID,
		FileName:         result.FileName,
		MimeType:         result.MimeType,
		SizeBytes:        result.SizeBytes,
		URL:              result.URL,
		Visibility:       result.Visibility,
		ProcessingStatus: result.ProcessingStatus,
		Version:          result.Version,
		ETag:             result.ETag,
		ContentHash:      result.ContentHash,
		CreatedAt:        result.CreatedAt,
		UpdatedAt:        result.UpdatedAt,
		Variants:         variants,
	}
}

type avatarFileTarget struct {
	bucket       string
	objectKey    string
	mimeType     string
	sizeBytes    int64
	etag         *string
	lastModified time.Time
	cacheID      string
}

func normalizeAvatarVariant(value string) string {
	switch strings.TrimSpace(value) {
	case "", "original":
		return strings.TrimSpace(value)
	case "thumb":
		return "thumbnail_512"
	default:
		return strings.TrimSpace(value)
	}
}

func avatarFileETag(target avatarFileTarget, version int) string {
	if target.etag != nil && strings.TrimSpace(*target.etag) != "" {
		return quoteHTTPETag(*target.etag)
	}
	return resourceETag("avatar-file", target.cacheID, version)
}

func quoteHTTPETag(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, `"`) && strings.HasSuffix(trimmed, `"`) {
		return trimmed
	}
	return `"` + strings.Trim(trimmed, `"`) + `"`
}

func setAvatarFileHeaders(w http.ResponseWriter, target avatarFileTarget, etag string) {
	header := w.Header()
	contentType := strings.TrimSpace(target.mimeType)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	header.Set("Content-Type", contentType)
	header.Set("Cache-Control", "public, max-age=31536000, immutable")
	if target.sizeBytes > 0 {
		header.Set("Content-Length", strconv.FormatInt(target.sizeBytes, 10))
	}
	if etag != "" {
		header.Set("ETag", etag)
	}
	if !target.lastModified.IsZero() {
		header.Set("Last-Modified", target.lastModified.UTC().Format(http.TimeFormat))
	}
}

func clearAvatarFileHeaders(w http.ResponseWriter) {
	header := w.Header()
	header.Del("Content-Type")
	header.Del("Content-Length")
	header.Del("Cache-Control")
	header.Del("ETag")
	header.Del("Last-Modified")
}

func writeFileError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, filesvc.ErrValidation):
		writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "Invalid file request")
	case errors.Is(err, filesvc.ErrFileTooLarge), errors.Is(err, filesvc.ErrFileQuotaExceeded):
		writeError(w, r, http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "File exceeds target size or user quota")
	case errors.Is(err, filesvc.ErrFileTypeNotAllowed):
		writeError(w, r, http.StatusUnsupportedMediaType, "FILE_TYPE_NOT_ALLOWED", "File type is not allowed")
	case errors.Is(err, filesvc.ErrFileScanRejected):
		writeError(w, r, http.StatusUnprocessableEntity, "FILE_SCAN_REJECTED", "File failed content scanning")
	case errors.Is(err, filesvc.ErrUploadMode):
		writeError(w, r, http.StatusBadRequest, "UPLOAD_MODE_INVALID", "Invalid upload mode")
	case errors.Is(err, filesvc.ErrStorageUnavailable):
		writeError(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "File storage is not configured")
	case errors.Is(err, filesvc.ErrForbidden):
		writeError(w, r, http.StatusForbidden, "FORBIDDEN", "Forbidden")
	case errors.Is(err, filesvc.ErrUploadExpired):
		writeError(w, r, http.StatusConflict, "UPLOAD_SESSION_EXPIRED", "Upload session expired")
	case errors.Is(err, filesvc.ErrUploadInvalid):
		writeError(w, r, http.StatusConflict, "UPLOAD_SESSION_INVALID", "Upload session is invalid")
	case errors.Is(err, filesvc.ErrUploadPartMissing):
		var partErr *filesvc.UploadPartError
		details := map[string]any{}
		if errors.As(err, &partErr) {
			details["missingPartNumbers"] = partErr.MissingPartNumbers
		}
		writeErrorDetails(w, r, http.StatusConflict, "UPLOAD_PART_MISSING", "Upload is missing required parts", details)
	case errors.Is(err, filesvc.ErrUploadPartETag):
		var partErr *filesvc.UploadPartError
		details := map[string]any{}
		if errors.As(err, &partErr) && len(partErr.PartNumbers) > 0 {
			details["partNumbers"] = partErr.PartNumbers
		}
		writeErrorDetails(w, r, http.StatusConflict, "UPLOAD_PART_ETAG_MISMATCH", "Upload part ETag mismatch", details)
	case errors.Is(err, filesvc.ErrContentHashMismatch):
		var hashErr *filesvc.ContentHashMismatchError
		details := map[string]any{}
		if errors.As(err, &hashErr) {
			details["expectedContentHash"] = hashErr.Expected
			details["actualContentHash"] = hashErr.Actual
		}
		writeErrorDetails(w, r, http.StatusConflict, "CONTENT_HASH_MISMATCH", "Content hash does not match uploaded object", details)
	case errors.Is(err, filesvc.ErrUploadCompleted):
		writeError(w, r, http.StatusConflict, "UPLOAD_SESSION_COMPLETED", "Upload session already completed")
	case errors.Is(err, filesvc.ErrUploadAborted):
		writeError(w, r, http.StatusConflict, "UPLOAD_SESSION_ABORTED", "Upload session already aborted")
	case errors.Is(err, filesvc.ErrFileReferenced):
		writeError(w, r, http.StatusConflict, "FILE_IN_USE", "File is referenced by a business resource")
	case errors.Is(err, repository.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Resource not found")
	case errors.Is(err, repository.ErrConflict):
		writeError(w, r, http.StatusConflict, "CONFLICT", "Resource conflict")
	default:
		writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Internal server error")
	}
}

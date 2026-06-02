package server

import (
	"errors"
	"net/http"
	"time"

	filesvc "nlserver/internal/files"
	"nlserver/internal/repository"
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

type okResponse struct {
	OK bool `json:"ok"`
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

	writeJSON(w, http.StatusOK, completeUploadSessionResponse{
		FileID:           result.FileID,
		URL:              result.URL,
		ETag:             result.ETag,
		ContentHash:      result.ContentHash,
		Version:          result.Version,
		ProcessingStatus: result.ProcessingStatus,
	})
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

func writeFileError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, filesvc.ErrValidation):
		writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "Invalid file request")
	case errors.Is(err, filesvc.ErrUploadMode):
		writeError(w, r, http.StatusBadRequest, "UPLOAD_MODE_INVALID", "Invalid upload mode")
	case errors.Is(err, filesvc.ErrStorageUnavailable):
		writeError(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "File storage is not configured")
	case errors.Is(err, filesvc.ErrForbidden):
		writeError(w, r, http.StatusForbidden, "FORBIDDEN", "Forbidden")
	case errors.Is(err, filesvc.ErrUploadExpired):
		writeError(w, r, http.StatusConflict, "UPLOAD_SESSION_EXPIRED", "Upload session expired")
	case errors.Is(err, filesvc.ErrUploadCompleted):
		writeError(w, r, http.StatusConflict, "UPLOAD_SESSION_COMPLETED", "Upload session already completed")
	case errors.Is(err, filesvc.ErrUploadAborted):
		writeError(w, r, http.StatusConflict, "UPLOAD_SESSION_ABORTED", "Upload session already aborted")
	case errors.Is(err, repository.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Resource not found")
	case errors.Is(err, repository.ErrConflict):
		writeError(w, r, http.StatusConflict, "CONFLICT", "Resource conflict")
	default:
		writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Internal server error")
	}
}

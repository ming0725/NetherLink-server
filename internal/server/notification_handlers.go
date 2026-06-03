package server

import (
	"encoding/json"
	"net/http"
	"time"

	"nlserver/internal/repository"
)

type notificationsResponse struct {
	Notifications []notificationResponse `json:"notifications"`
	Total         int                    `json:"total"`
	UnreadCount   int                    `json:"unreadCount"`
}

type notificationDetailResponse struct {
	Notification notificationResponse `json:"notification"`
}

type notificationResponse struct {
	NotificationID string          `json:"notificationId"`
	Type           string          `json:"type"`
	SourceID       *string         `json:"sourceId"`
	Payload        json.RawMessage `json:"payload"`
	ReadAt         *time.Time      `json:"readAt"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
}

type markNotificationsReadAllRequest struct {
	Type *string `json:"type"`
}

type markNotificationsReadAllResponse struct {
	OK          bool  `json:"ok"`
	Updated     int64 `json:"updated"`
	UnreadCount int   `json:"unreadCount"`
}

type notificationReadUpdatedPayload struct {
	Type         string                `json:"type,omitempty"`
	UnreadCount  int                   `json:"unreadCount"`
	Updated      int64                 `json:"updated,omitempty"`
	Notification *notificationResponse `json:"notification,omitempty"`
}

func (s *Server) handleNotifications(w http.ResponseWriter, r *http.Request) {
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	if s.repos.Notifications == nil {
		writeError(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "Notification repository is not configured")
		return
	}

	limit, offset, err := parsePagination(r)
	if err != nil {
		writeRequestError(w, r, err)
		return
	}

	notifications, total, unread, repoErr := s.repos.Notifications.List(r.Context(), principal.UserID, r.URL.Query().Get("type"), limit, offset)
	if repoErr != nil {
		writeRepositoryError(w, r, repoErr)
		return
	}

	writeJSON(w, http.StatusOK, notificationsResponse{
		Notifications: makeNotificationResponses(notifications),
		Total:         total,
		UnreadCount:   unread,
	})
}

func (s *Server) handleNotificationsReadAll(w http.ResponseWriter, r *http.Request) {
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	if s.repos.Notifications == nil {
		writeError(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "Notification repository is not configured")
		return
	}

	var req markNotificationsReadAllRequest
	if r.ContentLength != 0 {
		if err := decodeJSONBody(r, &req); err != nil {
			writeRequestError(w, r, err)
			return
		}
	}

	notificationType := ""
	if req.Type != nil {
		notificationType = *req.Type
	}
	updated, unread, err := s.repos.Notifications.MarkAllRead(r.Context(), principal.UserID, notificationType)
	if err != nil {
		writeRepositoryError(w, r, err)
		return
	}

	response := markNotificationsReadAllResponse{
		OK:          true,
		Updated:     updated,
		UnreadCount: unread,
	}
	s.publishSelfRealtimeEvent(r.Context(), principal.UserID, "notification.read.updated", notificationReadUpdatedPayload{
		Type:        notificationType,
		UnreadCount: unread,
		Updated:     updated,
	})
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleNotificationRead(w http.ResponseWriter, r *http.Request) {
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	if s.repos.Notifications == nil {
		writeError(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "Notification repository is not configured")
		return
	}

	notificationID := r.PathValue("notificationId")
	if err := validateUUID(notificationID, "notificationId"); err != nil {
		writeRequestError(w, r, err)
		return
	}

	notification, err := s.repos.Notifications.MarkRead(r.Context(), principal.UserID, notificationID)
	if err != nil {
		writeRepositoryError(w, r, err)
		return
	}

	notificationResponse := makeNotificationResponse(notification)
	unread, err := s.repos.Notifications.CountUnread(r.Context(), principal.UserID, notification.Type)
	if err != nil {
		writeRepositoryError(w, r, err)
		return
	}
	response := notificationDetailResponse{Notification: notificationResponse}
	s.publishSelfRealtimeEvent(r.Context(), principal.UserID, "notification.read.updated", notificationReadUpdatedPayload{
		Type:         notification.Type,
		UnreadCount:  unread,
		Notification: &notificationResponse,
	})
	writeJSON(w, http.StatusOK, response)
}

func makeNotificationResponses(notifications []repository.Notification) []notificationResponse {
	responses := make([]notificationResponse, 0, len(notifications))
	for _, notification := range notifications {
		responses = append(responses, makeNotificationResponse(notification))
	}
	return responses
}

func makeNotificationResponse(notification repository.Notification) notificationResponse {
	return notificationResponse{
		NotificationID: notification.ID,
		Type:           notification.Type,
		SourceID:       notification.SourceID,
		Payload:        notification.Payload,
		ReadAt:         notification.ReadAt,
		CreatedAt:      notification.CreatedAt,
		UpdatedAt:      notification.UpdatedAt,
	}
}

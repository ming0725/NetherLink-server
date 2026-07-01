package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	agentprofile "nlserver/internal/agent/profile"
	"nlserver/internal/repository"
)

type agentProfilesResponse struct {
	Entries []agentProfileResponse `json:"entries"`
}

type agentCreateRequest struct {
	Name              string          `json:"name"`
	AvatarFileID      *string         `json:"avatarFileId"`
	BasePrompt        string          `json:"basePrompt"`
	Model             *string         `json:"model"`
	ToolPolicy        json.RawMessage `json:"toolPolicy"`
	SafetyPolicy      json.RawMessage `json:"safetyPolicy"`
	ClientOperationID string          `json:"clientOperationId"`
}

type groupBotCreateRequest struct {
	Name              string          `json:"name"`
	AvatarFileID      *string         `json:"avatarFileId"`
	BasePrompt        string          `json:"basePrompt"`
	Model             *string         `json:"model"`
	ToolPolicy        json.RawMessage `json:"toolPolicy"`
	SafetyPolicy      json.RawMessage `json:"safetyPolicy"`
	CooldownSeconds   int             `json:"cooldownSeconds"`
	ClientOperationID string          `json:"clientOperationId"`
}

type agentProfileDetailResponse struct {
	Agent agentProfileResponse `json:"agent"`
}

type agentProfileResponse struct {
	AgentID        string          `json:"agentId"`
	BotUserID      string          `json:"botUserId"`
	OwnerUserID    *string         `json:"ownerUserId,omitempty"`
	Kind           string          `json:"kind"`
	Visibility     string          `json:"visibility"`
	Name           string          `json:"name"`
	AvatarFileID   *string         `json:"avatarFileId,omitempty"`
	BasePrompt     string          `json:"basePrompt"`
	Model          *string         `json:"model,omitempty"`
	ToolPolicy     json.RawMessage `json:"toolPolicy"`
	SafetyPolicy   json.RawMessage `json:"safetyPolicy"`
	StyleProfileID *string         `json:"styleProfileId,omitempty"`
	Status         string          `json:"status"`
	Version        int             `json:"version"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
}

func (s *Server) handleAgentList(w http.ResponseWriter, r *http.Request) {
	if !s.requireAgentService(w, r) {
		return
	}
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	limit, offset, reqErr := parsePagination(r)
	if reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}
	profiles, err := s.agentService.ListProfiles(r.Context(), agentprofile.ListProfilesInput{
		UserID: principal.UserID,
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		writeAgentServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, agentProfilesResponse{Entries: makeAgentProfileResponses(profiles)})
}

func (s *Server) handleAgentCreate(w http.ResponseWriter, r *http.Request) {
	if !s.requireAgentService(w, r) {
		return
	}
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	var req agentCreateRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeRequestError(w, r, err)
		return
	}
	if req.AvatarFileID != nil {
		if reqErr := validateOptionalUUID(*req.AvatarFileID, "avatarFileId"); reqErr != nil {
			writeRequestError(w, r, reqErr)
			return
		}
	}
	s.handleIdempotentWrite(w, r, principal.UserID, "agent.profile.create", idempotencyKeyFromRequest(r, req.ClientOperationID), func(err error) {
		writeAgentServiceError(w, r, err)
	}, func() (idempotentHTTPResult, error) {
		profile, err := s.agentService.CreateProfile(r.Context(), agentprofile.CreateProfileInput{
			UserID:       principal.UserID,
			Name:         req.Name,
			AvatarFileID: req.AvatarFileID,
			BasePrompt:   req.BasePrompt,
			Model:        req.Model,
			ToolPolicy:   req.ToolPolicy,
			SafetyPolicy: req.SafetyPolicy,
		})
		if err != nil {
			return idempotentHTTPResult{}, err
		}
		return idempotentHTTPResult{
			Status: http.StatusCreated,
			Body:   agentProfileDetailResponse{Agent: makeAgentProfileResponse(profile)},
		}, nil
	})
}

func (s *Server) handleCreateGroupBot(w http.ResponseWriter, r *http.Request) {
	if !s.requireAgentService(w, r) {
		return
	}
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	groupID := r.PathValue("groupId")
	if reqErr := validateUUID(groupID, "groupId"); reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}
	var req groupBotCreateRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeRequestError(w, r, err)
		return
	}
	if req.AvatarFileID != nil {
		if reqErr := validateOptionalUUID(*req.AvatarFileID, "avatarFileId"); reqErr != nil {
			writeRequestError(w, r, reqErr)
			return
		}
	}
	s.handleIdempotentWrite(w, r, principal.UserID, "group.bot.create:"+groupID, idempotencyKeyFromRequest(r, req.ClientOperationID), func(err error) {
		writeAgentServiceError(w, r, err)
	}, func() (idempotentHTTPResult, error) {
		profile, err := s.agentService.CreateGroupBot(r.Context(), agentprofile.CreateGroupBotInput{
			UserID:          principal.UserID,
			GroupID:         groupID,
			Name:            req.Name,
			AvatarFileID:    req.AvatarFileID,
			BasePrompt:      req.BasePrompt,
			Model:           req.Model,
			ToolPolicy:      req.ToolPolicy,
			SafetyPolicy:    req.SafetyPolicy,
			CooldownSeconds: req.CooldownSeconds,
		})
		if err != nil {
			return idempotentHTTPResult{}, err
		}
		response := agentProfileDetailResponse{Agent: makeAgentProfileResponse(profile)}
		s.publishGroupRealtimeEvent(r.Context(), groupID, "group.bot.created", response)
		return idempotentHTTPResult{
			Status: http.StatusCreated,
			Body:   response,
		}, nil
	})
}

func (s *Server) handleAgentGet(w http.ResponseWriter, r *http.Request) {
	if !s.requireAgentService(w, r) {
		return
	}
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	agentID := r.PathValue("agentId")
	if reqErr := validateUUID(agentID, "agentId"); reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}
	profile, err := s.agentService.GetProfile(r.Context(), agentprofile.GetProfileInput{
		UserID:  principal.UserID,
		AgentID: agentID,
	})
	if err != nil {
		writeAgentServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, agentProfileDetailResponse{Agent: makeAgentProfileResponse(profile)})
}

func (s *Server) handleAgentDelete(w http.ResponseWriter, r *http.Request) {
	if !s.requireAgentService(w, r) {
		return
	}
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	agentID := r.PathValue("agentId")
	if reqErr := validateUUID(agentID, "agentId"); reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}
	s.handleIdempotentWrite(w, r, principal.UserID, "agent.profile.delete:"+agentID, idempotencyKeyFromRequest(r, r.URL.Query().Get("clientOperationId")), func(err error) {
		writeAgentServiceError(w, r, err)
	}, func() (idempotentHTTPResult, error) {
		if err := s.agentService.DeleteProfile(r.Context(), agentprofile.DeleteProfileInput{
			UserID:  principal.UserID,
			AgentID: agentID,
		}); err != nil {
			return idempotentHTTPResult{}, err
		}
		return idempotentHTTPResult{
			Status: http.StatusOK,
			Body:   map[string]bool{"ok": true},
		}, nil
	})
}

func makeAgentProfileResponses(profiles []repository.AgentProfile) []agentProfileResponse {
	responses := make([]agentProfileResponse, 0, len(profiles))
	for _, profile := range profiles {
		responses = append(responses, makeAgentProfileResponse(profile))
	}
	return responses
}

func makeAgentProfileResponse(profile repository.AgentProfile) agentProfileResponse {
	return agentProfileResponse{
		AgentID:        profile.ID,
		BotUserID:      profile.BotUserID,
		OwnerUserID:    profile.OwnerUserID,
		Kind:           profile.Kind,
		Visibility:     profile.Visibility,
		Name:           profile.Name,
		AvatarFileID:   profile.AvatarFileID,
		BasePrompt:     profile.BasePrompt,
		Model:          profile.Model,
		ToolPolicy:     profile.ToolPolicy,
		SafetyPolicy:   profile.SafetyPolicy,
		StyleProfileID: profile.StyleProfileID,
		Status:         profile.Status,
		Version:        profile.Version,
		CreatedAt:      profile.CreatedAt,
		UpdatedAt:      profile.UpdatedAt,
	}
}

func (s *Server) requireAgentService(w http.ResponseWriter, r *http.Request) bool {
	if s.agentService != nil {
		return true
	}
	writeError(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "Agent service is not configured")
	return false
}

func writeAgentServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, agentprofile.ErrValidation):
		writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "Invalid agent request")
	case errors.Is(err, agentprofile.ErrForbidden):
		writeError(w, r, http.StatusForbidden, "FORBIDDEN", "Forbidden")
	case errors.Is(err, agentprofile.ErrUnavailable):
		writeError(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "Agent service is not configured")
	case errors.Is(err, repository.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Resource not found")
	default:
		writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Internal server error")
	}
}

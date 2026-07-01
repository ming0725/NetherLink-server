package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	friendsvc "nlserver/internal/friend"
	groupsvc "nlserver/internal/group"
	"nlserver/internal/repository"
)

const maxBatchUsers = 100

type batchUsersRequest struct {
	UserUUIDs []string               `json:"userUuids"`
	Items     []batchUserRequestItem `json:"items"`
}

type batchPresenceRequest struct {
	UserUUIDs []string `json:"userUuids"`
}

type batchUserRequestItem struct {
	UserUUID     string `json:"userUuid"`
	KnownVersion *int   `json:"knownVersion"`
}

type batchGroupMembersRequest struct {
	UserUUIDs []string                      `json:"userUuids"`
	Items     []batchGroupMemberRequestItem `json:"items"`
}

type batchGroupMemberRequestItem struct {
	UserUUID     string `json:"userUuid"`
	KnownVersion *int   `json:"knownVersion"`
}

type createFriendGroupRequest struct {
	Name              string `json:"name"`
	SortOrder         int    `json:"sortOrder"`
	ClientOperationID string `json:"clientOperationId"`
}

type patchFriendGroupRequest struct {
	Name              *string `json:"name"`
	SortOrder         *int    `json:"sortOrder"`
	ClientOperationID string  `json:"clientOperationId"`
}

type patchFriendRequest struct {
	Remark            *string `json:"remark"`
	GroupID           *string `json:"groupId"`
	FriendGroupID     *string `json:"friendGroupId"`
	IsDND             *bool   `json:"isDnd"`
	ClientOperationID string  `json:"clientOperationId"`
}

type createFriendRequestRequest struct {
	ToUserUUID        string  `json:"toUserUuid"`
	ToUserID          string  `json:"toUserId"`
	Message           *string `json:"message"`
	SourceType        *string `json:"sourceType"`
	SourceGroupID     *string `json:"sourceGroupId"`
	SourceFriendUUID  *string `json:"sourceFriendUuid"`
	ClientOperationID string  `json:"clientOperationId"`
}

type acceptFriendRequestRequest struct {
	Remark            *string `json:"remark"`
	GroupID           *string `json:"groupId"`
	FriendGroupID     *string `json:"friendGroupId"`
	ClientOperationID string  `json:"clientOperationId"`
}

type rejectFriendRequestRequest struct {
	ClientOperationID string `json:"clientOperationId"`
}

type createGroupRequest struct {
	Name              string   `json:"name"`
	MemberIDs         []string `json:"memberIds"`
	AvatarFileID      *string  `json:"avatarFileId"`
	ClientOperationID string   `json:"clientOperationId"`
}

type patchGroupRequest struct {
	Name              *string             `json:"name"`
	Introduction      *string             `json:"introduction"`
	Announcement      *string             `json:"announcement"`
	AvatarFileID      optionalStringField `json:"avatarFileId"`
	ExpectedVersion   *int                `json:"expectedVersion"`
	ClientOperationID string              `json:"clientOperationId"`
}

type addGroupMembersRequest struct {
	UserUUIDs         []string `json:"userUuids"`
	ClientOperationID string   `json:"clientOperationId"`
}

type patchGroupMemberRequest struct {
	Role              *string `json:"role"`
	Nickname          *string `json:"nickname"`
	IsDND             *bool   `json:"isDnd"`
	ClientOperationID string  `json:"clientOperationId"`
}

type transferGroupOwnerRequest struct {
	UserUUID          string `json:"userUuid"`
	ClientOperationID string `json:"clientOperationId"`
}

type patchGroupMySettingsRequest struct {
	Remark            *string `json:"remark"`
	ListGroupID       *string `json:"listGroupId"`
	ListGroupName     *string `json:"listGroupName"`
	IsDND             *bool   `json:"isDnd"`
	ClientOperationID string  `json:"clientOperationId"`
}

type createGroupJoinRequestRequest struct {
	GroupID           string  `json:"groupId"`
	GroupPublicID     string  `json:"groupPublicId"`
	Message           *string `json:"message"`
	ClientOperationID string  `json:"clientOperationId"`
}

type usersListResponse struct {
	Users []userResponse `json:"users"`
	Total int            `json:"total"`
}

type userDetailResponse struct {
	User     userResponse            `json:"user"`
	Relation *friendRelationResponse `json:"relation,omitempty"`
}

type friendRelationResponse struct {
	IsFriend        bool    `json:"isFriend"`
	Remark          *string `json:"remark"`
	FriendGroupID   *string `json:"friendGroupId"`
	FriendGroupName *string `json:"friendGroupName,omitempty"`
	IsDND           bool    `json:"isDnd"`
	Version         int     `json:"version,omitempty"`
}

type batchUsersResponse struct {
	Users       []userResponse `json:"users"`
	NotModified []string       `json:"notModified,omitempty"`
	NotFound    []string       `json:"notFound,omitempty"`
}

type presenceEntryResponse struct {
	UserUUID   string     `json:"userUuid"`
	Status     string     `json:"status"`
	LastSeenAt *time.Time `json:"lastSeenAt"`
	UpdatedAt  *time.Time `json:"updatedAt,omitempty"`
}

type batchPresenceResponse struct {
	Presences []presenceEntryResponse `json:"presences"`
	NotFound  []string                `json:"notFound,omitempty"`
}

type friendGroupsResponse struct {
	Groups []friendGroupResponse `json:"groups"`
}

type friendGroupDetailResponse struct {
	Group friendGroupResponse `json:"group"`
}

type friendGroupResponse struct {
	GroupID       string    `json:"groupId"`
	FriendGroupID string    `json:"friendGroupId"`
	Name          string    `json:"name"`
	SortOrder     int       `json:"sortOrder"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type friendsResponse struct {
	Friends []friendResponse `json:"friends"`
	Total   int              `json:"total"`
}

type friendshipResponse struct {
	Friendship friendResponse `json:"friendship"`
}

type friendResponse struct {
	User            userResponse `json:"user"`
	Remark          *string      `json:"remark"`
	FriendGroupID   *string      `json:"friendGroupId"`
	FriendGroupName *string      `json:"friendGroupName,omitempty"`
	IsDND           bool         `json:"isDnd"`
	Version         int          `json:"version"`
	CreatedAt       time.Time    `json:"createdAt"`
	UpdatedAt       time.Time    `json:"updatedAt"`
}

type friendRequestsResponse struct {
	Requests    []friendRequestResponse `json:"requests"`
	Total       int                     `json:"total"`
	UnreadCount int                     `json:"unreadCount"`
}

type friendRequestDetailResponse struct {
	Request friendRequestResponse `json:"request"`
}

type friendRequestActionErrorDetails struct {
	RequestedAction string                `json:"requestedAction"`
	ActorRole       string                `json:"actorRole"`
	CurrentStatus   string                `json:"currentStatus"`
	Request         friendRequestResponse `json:"request"`
}

type friendRequestRealtimePayload struct {
	Request      friendRequestResponse        `json:"request"`
	Notification friendRequestResponse        `json:"notification"`
	Refs         *referencedResourcesResponse `json:"refs,omitempty"`
}

type friendRequestResponse struct {
	RequestID      string       `json:"requestId"`
	FromUser       userResponse `json:"fromUser"`
	ToUser         userResponse `json:"toUser"`
	Message        *string      `json:"message"`
	SourceType     *string      `json:"sourceType"`
	SourceGroupID  *string      `json:"sourceGroupId"`
	SourceFriendID *string      `json:"sourceFriendUuid"`
	Status         string       `json:"status"`
	HandledAt      *time.Time   `json:"handledAt"`
	CreatedAt      time.Time    `json:"createdAt"`
	UpdatedAt      time.Time    `json:"updatedAt"`
}

type groupsResponse struct {
	Groups []groupResponse `json:"groups"`
	Total  int             `json:"total"`
}

type groupDetailResponse struct {
	Group         groupResponse `json:"group"`
	ChangedFields []string      `json:"changedFields,omitempty"`
	AvatarChanged *bool         `json:"avatarChanged,omitempty"`
}

type groupSettingsDetailResponse struct {
	Settings groupSettingsResponse `json:"settings"`
}

type groupResponse struct {
	GroupID           string    `json:"groupId"`
	GroupPublicID     string    `json:"groupPublicId"`
	Name              string    `json:"name"`
	OwnerUUID         string    `json:"ownerUuid"`
	AvatarURL         *string   `json:"avatarUrl"`
	AvatarVersion     int       `json:"avatarVersion"`
	AvatarEtag        *string   `json:"avatarEtag"`
	AvatarContentHash *string   `json:"avatarContentHash"`
	Introduction      *string   `json:"introduction"`
	Announcement      *string   `json:"announcement"`
	MemberCount       int       `json:"memberCount"`
	Role              string    `json:"role"`
	Nickname          *string   `json:"nickname"`
	IsDND             bool      `json:"isDnd"`
	Remark            *string   `json:"remark"`
	ListGroupID       *string   `json:"listGroupId"`
	ListGroupName     *string   `json:"listGroupName"`
	Version           int       `json:"version"`
	ETag              string    `json:"etag"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

type groupMembersResponse struct {
	Members []groupMemberResponse `json:"members"`
	Total   int                   `json:"total"`
}

type batchGroupMembersResponse struct {
	Members     []groupMemberResponse `json:"members"`
	NotModified []string              `json:"notModified,omitempty"`
	NotFound    []string              `json:"notFound,omitempty"`
}

type groupMemberDetailResponse struct {
	Member groupMemberResponse `json:"member"`
}

type groupMemberResponse struct {
	User     userResponse `json:"user"`
	Role     string       `json:"role"`
	Nickname *string      `json:"nickname"`
	IsDND    bool         `json:"isDnd"`
	JoinedAt time.Time    `json:"joinedAt"`
	Version  int          `json:"version"`
}

type groupSettingsResponse struct {
	GroupID       string    `json:"groupId"`
	Remark        *string   `json:"remark"`
	ListGroupID   *string   `json:"listGroupId"`
	ListGroupName *string   `json:"listGroupName"`
	IsDND         bool      `json:"isDnd"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type groupJoinRequestsResponse struct {
	Notifications []groupJoinRequestResponse `json:"notifications"`
	Total         int                        `json:"total"`
	UnreadCount   int                        `json:"unreadCount"`
}

type groupJoinRequestDetailResponse struct {
	Request groupJoinRequestResponse `json:"request"`
}

type groupJoinRequestResponse struct {
	RequestID    string        `json:"requestId"`
	Group        groupResponse `json:"group"`
	ActorUser    userResponse  `json:"actorUser"`
	OperatorUser *userResponse `json:"operatorUser,omitempty"`
	Message      *string       `json:"message"`
	Status       string        `json:"status"`
	HandledAt    *time.Time    `json:"handledAt"`
	CreatedAt    time.Time     `json:"createdAt"`
	UpdatedAt    time.Time     `json:"updatedAt"`
}

type optionalStringInput struct {
	Set   bool
	Value *string
}

type optionalStringField struct {
	Set   bool
	Value *string
}

func (f *optionalStringField) UnmarshalJSON(data []byte) error {
	f.Set = true
	if string(data) == "null" {
		f.Value = nil
		return nil
	}
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	f.Value = &value
	return nil
}

func boolPtr(value bool) *bool {
	return &value
}

func groupPatchChangedFields(req patchGroupRequest, current repository.GroupListEntry, updated repository.GroupListEntry) ([]string, bool) {
	changedFields := []string{}
	if req.Name != nil && current.Group.Name != updated.Group.Name {
		changedFields = append(changedFields, "name")
	}
	if req.Introduction != nil && !optionalStringEqual(current.Group.Introduction, updated.Group.Introduction) {
		changedFields = append(changedFields, "introduction")
	}
	if req.Announcement != nil && !optionalStringEqual(current.Group.Announcement, updated.Group.Announcement) {
		changedFields = append(changedFields, "announcement")
	}
	avatarChanged := false
	if req.AvatarFileID.Set {
		avatarChanged = !optionalStringEqual(current.Group.AvatarFileID, updated.Group.AvatarFileID)
		if avatarChanged {
			changedFields = append(changedFields, "avatarFileId")
		}
	}
	return changedFields, avatarChanged
}

func optionalStringEqual(a *string, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func parseOptionalFriendGroupID(groupID *string, friendGroupID *string) (optionalStringInput, *requestError) {
	if groupID == nil && friendGroupID == nil {
		return optionalStringInput{}, nil
	}

	if groupID != nil && friendGroupID != nil && strings.TrimSpace(*groupID) != strings.TrimSpace(*friendGroupID) {
		return optionalStringInput{}, &requestError{
			Status:  http.StatusBadRequest,
			Code:    "VALIDATION_ERROR",
			Message: "use either friendGroupId or groupId, not both",
		}
	}

	source := groupID
	field := "groupId"
	if friendGroupID != nil {
		source = friendGroupID
		field = "friendGroupId"
	}

	normalized := strings.TrimSpace(*source)
	if normalized != "" && !uuidPattern.MatchString(normalized) {
		return optionalStringInput{}, &requestError{
			Status:  http.StatusBadRequest,
			Code:    "VALIDATION_ERROR",
			Message: field + " must be a friend group UUID",
		}
	}

	return optionalStringInput{Set: true, Value: &normalized}, nil
}

func parseAcceptFriendGroupID(groupID *string, friendGroupID *string) (optionalStringInput, *requestError) {
	if friendGroupID != nil {
		return parseOptionalFriendGroupID(nil, friendGroupID)
	}
	if groupID == nil {
		return optionalStringInput{}, nil
	}
	if normalized := strings.TrimSpace(*groupID); normalized != "" && !uuidPattern.MatchString(normalized) {
		return optionalStringInput{}, nil
	}
	return parseOptionalFriendGroupID(groupID, nil)
}

func validateOptionalGroupRoleFilter(role string) *requestError {
	switch strings.TrimSpace(role) {
	case "", "joined", "created", "owner", "managed", "admin", "ai", "member":
		return nil
	default:
		return &requestError{
			Status:  http.StatusBadRequest,
			Code:    "VALIDATION_ERROR",
			Message: "role must be one of joined, created, owner, managed, admin, ai, or member",
		}
	}
}

func (s *Server) handleUsersSearch(w http.ResponseWriter, r *http.Request) {
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}

	limit, offset, err := parsePagination(r)
	if err != nil {
		writeRequestError(w, r, err)
		return
	}

	users, total, repoErr := s.repos.Users.Search(r.Context(), r.URL.Query().Get("keyword"), limit, offset)
	if repoErr != nil {
		writeRepositoryError(w, r, repoErr)
		return
	}

	writeJSON(w, http.StatusOK, usersListResponse{
		Users: s.makePublicUserResponsesForViewer(principal.UserID, users),
		Total: total,
	})
}

func (s *Server) handleUserByUUID(w http.ResponseWriter, r *http.Request) {
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}

	userUUID := r.PathValue("userUuid")
	if err := validateUUID(userUUID, "userUuid"); err != nil {
		writeRequestError(w, r, err)
		return
	}

	user, err := s.repos.Users.GetByID(r.Context(), userUUID)
	if err != nil {
		writeRepositoryError(w, r, err)
		return
	}

	response, err := s.makeUserDetailResponse(r.Context(), principal.UserID, user)
	if err != nil {
		writeRepositoryError(w, r, err)
		return
	}

	writeJSONWithETag(w, r, http.StatusOK, response.User.ETag, response)
}

func (s *Server) handleUserByPublicID(w http.ResponseWriter, r *http.Request) {
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}

	user, err := s.repos.Users.GetByPublicID(r.Context(), r.PathValue("userId"))
	if err != nil {
		writeRepositoryError(w, r, err)
		return
	}

	response, err := s.makeUserDetailResponse(r.Context(), principal.UserID, user)
	if err != nil {
		writeRepositoryError(w, r, err)
		return
	}

	writeJSONWithETag(w, r, http.StatusOK, response.User.ETag, response)
}

func (s *Server) handleUsersBatch(w http.ResponseWriter, r *http.Request) {
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}

	var req batchUsersRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeRequestError(w, r, err)
		return
	}

	lookups, reqErr := collectBatchUserLookups(req)
	if reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}
	userIDs := make([]string, 0, len(lookups))
	for _, lookup := range lookups {
		userIDs = append(userIDs, lookup.UserUUID)
	}

	users, err := s.repos.Users.BatchGetByIDs(r.Context(), userIDs)
	if err != nil {
		writeRepositoryError(w, r, err)
		return
	}

	usersByID := make(map[string]repository.User, len(users))
	for _, user := range users {
		usersByID[user.ID] = user
	}
	response := batchUsersResponse{Users: []userResponse{}}
	for _, lookup := range lookups {
		user, ok := usersByID[lookup.UserUUID]
		if !ok {
			response.NotFound = append(response.NotFound, lookup.UserUUID)
			continue
		}
		if lookup.HasKnownVersion && user.Version <= lookup.KnownVersion {
			response.NotModified = append(response.NotModified, lookup.UserUUID)
			continue
		}
		response.Users = append(response.Users, s.makePublicUserResponseForViewer(principal.UserID, user))
	}

	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handlePresenceBatch(w http.ResponseWriter, r *http.Request) {
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}

	var req batchPresenceRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeRequestError(w, r, err)
		return
	}
	userIDs := uniqueStrings(req.UserUUIDs)
	if len(userIDs) > maxBatchUsers {
		writeRequestError(w, r, &requestError{
			Status:  http.StatusBadRequest,
			Code:    "VALIDATION_ERROR",
			Message: "userUuids exceeds maximum batch size",
		})
		return
	}
	for _, userID := range userIDs {
		if reqErr := validateUUID(userID, "userUuids"); reqErr != nil {
			writeRequestError(w, r, reqErr)
			return
		}
	}

	users, err := s.repos.Users.BatchGetByIDs(r.Context(), userIDs)
	if err != nil {
		writeRepositoryError(w, r, err)
		return
	}
	usersByID := make(map[string]repository.User, len(users))
	for _, user := range users {
		usersByID[user.ID] = user
	}

	response := batchPresenceResponse{Presences: []presenceEntryResponse{}}
	for _, userID := range userIDs {
		user, ok := usersByID[userID]
		if !ok {
			response.NotFound = append(response.NotFound, userID)
			continue
		}
		presence := s.presenceForViewer(principal.UserID, user.ID, user.LastSeenAt)
		response.Presences = append(response.Presences, presenceEntryResponse{
			UserUUID:   user.ID,
			Status:     presence.Status,
			LastSeenAt: presence.LastSeenAt,
			UpdatedAt:  presence.UpdatedAt,
		})
	}

	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleFriendGroups(w http.ResponseWriter, r *http.Request) {
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}

	groups, err := s.repos.Friends.ListGroups(r.Context(), principal.UserID, r.URL.Query().Get("keyword"))
	if err != nil {
		writeRepositoryError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, friendGroupsResponse{Groups: makeFriendGroupResponses(groups)})
}

func (s *Server) handleCreateFriendGroup(w http.ResponseWriter, r *http.Request) {
	if !s.requireFriendService(w, r) {
		return
	}
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}

	var req createFriendGroupRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeRequestError(w, r, err)
		return
	}

	s.handleIdempotentWrite(w, r, principal.UserID, "friend.group.create", idempotencyKeyFromRequest(r, req.ClientOperationID), func(err error) {
		writeFriendServiceError(w, r, err)
	}, func() (idempotentHTTPResult, error) {
		group, err := s.friendService.CreateGroup(r.Context(), friendsvc.CreateGroupInput{
			UserID:    principal.UserID,
			Name:      req.Name,
			SortOrder: req.SortOrder,
		})
		if err != nil {
			return idempotentHTTPResult{}, err
		}
		response := friendGroupDetailResponse{Group: makeFriendGroupResponse(group)}
		return idempotentHTTPResult{
			Status: http.StatusCreated,
			Body:   response,
			After: func() {
				s.publishSelfRealtimeEvent(r.Context(), principal.UserID, "friend.group.created", response)
			},
		}, nil
	})
}

func (s *Server) handlePatchFriendGroup(w http.ResponseWriter, r *http.Request) {
	if !s.requireFriendService(w, r) {
		return
	}
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}

	friendGroupID := r.PathValue("friendGroupId")
	if reqErr := validateUUID(friendGroupID, "friendGroupId"); reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}

	var req patchFriendGroupRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeRequestError(w, r, err)
		return
	}

	s.handleIdempotentWrite(w, r, principal.UserID, "friend.group.update:"+friendGroupID, idempotencyKeyFromRequest(r, req.ClientOperationID), func(err error) {
		writeFriendServiceError(w, r, err)
	}, func() (idempotentHTTPResult, error) {
		group, err := s.friendService.UpdateGroup(r.Context(), friendsvc.UpdateGroupInput{
			UserID:        principal.UserID,
			FriendGroupID: friendGroupID,
			Name:          req.Name,
			SortOrder:     req.SortOrder,
		})
		if err != nil {
			return idempotentHTTPResult{}, err
		}
		response := friendGroupDetailResponse{Group: makeFriendGroupResponse(group)}
		return idempotentHTTPResult{
			Status: http.StatusOK,
			Body:   response,
			After: func() {
				s.publishSelfRealtimeEvent(r.Context(), principal.UserID, "friend.group.updated", response)
			},
		}, nil
	})
}

func (s *Server) handleDeleteFriendGroup(w http.ResponseWriter, r *http.Request) {
	if !s.requireFriendService(w, r) {
		return
	}
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}

	friendGroupID := r.PathValue("friendGroupId")
	if reqErr := validateUUID(friendGroupID, "friendGroupId"); reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}

	s.handleIdempotentWrite(w, r, principal.UserID, "friend.group.delete:"+friendGroupID, idempotencyKeyFromRequest(r, r.URL.Query().Get("clientOperationId")), func(err error) {
		writeFriendServiceError(w, r, err)
	}, func() (idempotentHTTPResult, error) {
		if err := s.friendService.DeleteGroup(r.Context(), principal.UserID, friendGroupID); err != nil {
			return idempotentHTTPResult{}, err
		}
		payload := map[string]any{
			"groupId":       friendGroupID,
			"friendGroupId": friendGroupID,
		}
		return idempotentHTTPResult{
			Status: http.StatusOK,
			Body:   okResponse{OK: true},
			After: func() {
				s.publishSelfRealtimeEvent(r.Context(), principal.UserID, "friend.group.deleted", payload)
			},
		}, nil
	})
}

func (s *Server) handleFriends(w http.ResponseWriter, r *http.Request) {
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
	if reqErr := validateOptionalUUID(r.URL.Query().Get("groupId"), "groupId"); reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}

	friends, total, err := s.repos.Friends.ListFriends(
		r.Context(),
		principal.UserID,
		r.URL.Query().Get("keyword"),
		r.URL.Query().Get("groupId"),
		limit,
		offset,
	)
	if err != nil {
		writeRepositoryError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, friendsResponse{Friends: s.makeFriendResponsesForViewer(principal.UserID, friends), Total: total})
}

func (s *Server) handlePatchFriend(w http.ResponseWriter, r *http.Request) {
	if !s.requireFriendService(w, r) {
		return
	}
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}

	friendUserID := r.PathValue("friendUserUuid")
	if reqErr := validateUUID(friendUserID, "friendUserUuid"); reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}

	var req patchFriendRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeRequestError(w, r, err)
		return
	}
	friendGroupID, reqErr := parseOptionalFriendGroupID(req.GroupID, req.FriendGroupID)
	if reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}

	s.handleIdempotentWrite(w, r, principal.UserID, "friend.update:"+friendUserID, idempotencyKeyFromRequest(r, req.ClientOperationID), func(err error) {
		writeFriendServiceError(w, r, err)
	}, func() (idempotentHTTPResult, error) {
		friend, err := s.friendService.UpdateFriend(r.Context(), friendsvc.UpdateFriendInput{
			UserID:       principal.UserID,
			FriendUserID: friendUserID,
			RemarkSet:    req.Remark != nil,
			Remark:       req.Remark,
			GroupIDSet:   friendGroupID.Set,
			GroupID:      friendGroupID.Value,
			IsDND:        req.IsDND,
		})
		if err != nil {
			return idempotentHTTPResult{}, err
		}
		response := friendshipResponse{Friendship: s.makeFriendResponseForViewer(principal.UserID, friend)}
		return idempotentHTTPResult{
			Status: http.StatusOK,
			Body:   response,
			After: func() {
				s.publishSelfRealtimeEvent(r.Context(), principal.UserID, "friend.updated", response)
			},
		}, nil
	})
}

func (s *Server) handleDeleteFriend(w http.ResponseWriter, r *http.Request) {
	if !s.requireFriendService(w, r) {
		return
	}
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}

	friendUserID := r.PathValue("friendUserUuid")
	if reqErr := validateUUID(friendUserID, "friendUserUuid"); reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}

	s.handleIdempotentWrite(w, r, principal.UserID, "friend.delete:"+friendUserID, idempotencyKeyFromRequest(r, r.URL.Query().Get("clientOperationId")), func(err error) {
		writeFriendServiceError(w, r, err)
	}, func() (idempotentHTTPResult, error) {
		if err := s.friendService.DeleteFriend(r.Context(), principal.UserID, friendUserID); err != nil {
			return idempotentHTTPResult{}, err
		}
		payload := map[string]any{
			"userUuid":       principal.UserID,
			"friendUserUuid": friendUserID,
		}
		return idempotentHTTPResult{
			Status: http.StatusOK,
			Body:   okResponse{OK: true},
			After: func() {
				s.publishRealtimeEvent(r.Context(), []string{principal.UserID, friendUserID}, "friend.deleted", payload)
			},
		}, nil
	})
}

func (s *Server) handleCreateFriendRequest(w http.ResponseWriter, r *http.Request) {
	if !s.requireFriendService(w, r) {
		return
	}
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}

	var req createFriendRequestRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeRequestError(w, r, err)
		return
	}
	toUserUUID, reqErr := s.resolveFriendRequestTargetUserUUID(r.Context(), req)
	if reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}
	if req.SourceGroupID != nil {
		if reqErr := validateOptionalUUID(*req.SourceGroupID, "sourceGroupId"); reqErr != nil {
			writeRequestError(w, r, reqErr)
			return
		}
	}
	if req.SourceFriendUUID != nil {
		if reqErr := validateOptionalUUID(*req.SourceFriendUUID, "sourceFriendUuid"); reqErr != nil {
			writeRequestError(w, r, reqErr)
			return
		}
	}

	s.handleIdempotentWrite(w, r, principal.UserID, "friend.request.create:"+toUserUUID, idempotencyKeyFromRequest(r, req.ClientOperationID), func(err error) {
		writeFriendServiceError(w, r, err)
	}, func() (idempotentHTTPResult, error) {
		request, err := s.friendService.CreateRequest(r.Context(), friendsvc.CreateRequestInput{
			FromUserID:     principal.UserID,
			ToUserID:       toUserUUID,
			Message:        req.Message,
			SourceType:     req.SourceType,
			SourceGroupID:  req.SourceGroupID,
			SourceFriendID: req.SourceFriendUUID,
		})
		if err != nil {
			return idempotentHTTPResult{}, err
		}
		response := friendRequestDetailResponse{Request: makeFriendRequestResponse(request)}
		return idempotentHTTPResult{
			Status: http.StatusCreated,
			Body:   response,
			After: func() {
				refs, err := s.makeUserRefsForIDs(r.Context(), []string{request.FromUser.ID, request.ToUser.ID})
				if err != nil {
					s.logger.WarnContext(r.Context(), "failed to load friend request user refs", "error", err, "request_id", request.ID)
				}
				s.publishRealtimeEvent(r.Context(), []string{request.FromUser.ID, request.ToUser.ID}, "friend.request.created", friendRequestRealtimePayload{
					Request:      response.Request,
					Notification: response.Request,
					Refs:         refs,
				})
			},
		}, nil
	})
}

func (s *Server) resolveFriendRequestTargetUserUUID(ctx context.Context, req createFriendRequestRequest) (string, *requestError) {
	toUserUUID := strings.TrimSpace(req.ToUserUUID)
	toUserID := strings.TrimSpace(req.ToUserID)
	if toUserUUID == "" && toUserID == "" {
		return "", &requestError{
			Status:  http.StatusBadRequest,
			Code:    "VALIDATION_ERROR",
			Message: "toUserUuid or toUserId is required",
		}
	}
	if toUserUUID != "" && toUserID != "" {
		return "", &requestError{
			Status:  http.StatusBadRequest,
			Code:    "VALIDATION_ERROR",
			Message: "use either toUserUuid or toUserId, not both",
		}
	}
	if toUserUUID != "" {
		if reqErr := validateUUID(toUserUUID, "toUserUuid"); reqErr != nil {
			return "", reqErr
		}
		return toUserUUID, nil
	}

	user, err := s.repos.Users.GetByPublicID(ctx, toUserID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return "", &requestError{
				Status:  http.StatusNotFound,
				Code:    "NOT_FOUND",
				Message: "target user not found",
			}
		}
		return "", &requestError{
			Status:  http.StatusInternalServerError,
			Code:    "INTERNAL_ERROR",
			Message: "failed to resolve target user",
		}
	}
	return user.ID, nil
}

func (s *Server) handleFriendRequests(w http.ResponseWriter, r *http.Request) {
	if !s.requireFriendService(w, r) {
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

	requests, total, unread, err := s.friendService.ListRequests(r.Context(), friendsvc.ListRequestsInput{
		UserID: principal.UserID,
		Status: r.URL.Query().Get("status"),
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		writeFriendServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, friendRequestsResponse{
		Requests:    makeFriendRequestResponses(requests),
		Total:       total,
		UnreadCount: unread,
	})
}

func (s *Server) handleAcceptFriendRequest(w http.ResponseWriter, r *http.Request) {
	if !s.requireFriendService(w, r) {
		return
	}
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}

	requestID := r.PathValue("requestId")
	if reqErr := validateUUID(requestID, "requestId"); reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}

	var req acceptFriendRequestRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeRequestError(w, r, err)
		return
	}
	friendGroupID, reqErr := parseAcceptFriendGroupID(req.GroupID, req.FriendGroupID)
	if reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}

	s.handleIdempotentWrite(w, r, principal.UserID, "friend.request.accept:"+requestID, idempotencyKeyFromRequest(r, req.ClientOperationID), func(err error) {
		writeFriendServiceError(w, r, err)
	}, func() (idempotentHTTPResult, error) {
		result, err := s.friendService.AcceptRequest(r.Context(), friendsvc.AcceptRequestInput{
			UserID:    principal.UserID,
			RequestID: requestID,
			Remark:    req.Remark,
			GroupID:   friendGroupID.Value,
		})
		if err != nil {
			return idempotentHTTPResult{}, err
		}
		response := friendshipResponse{Friendship: s.makeFriendResponseForViewer(principal.UserID, result.Friend)}
		return idempotentHTTPResult{
			Status: http.StatusOK,
			Body:   response,
			After: func() {
				s.publishRealtimeEvent(r.Context(), []string{principal.UserID, result.Friend.User.ID}, "friend.request.accepted", response)
				if result.Message == nil || result.ConversationID == "" {
					return
				}
				messageResponse, err := s.makeMessageResponse(r, *result.Message)
				if err != nil {
					s.logger.WarnContext(r.Context(), "failed to build friend accepted message response", "error", err, "request_id", requestID)
					return
				}
				s.publishConversationMessage(r.Context(), result.ConversationID, "chat.message.created", messageResponse)
				s.publishAllConversationSyncStates(r.Context(), result.ConversationID, "friend.request.accepted")
				s.publishAllConversationUpdated(r.Context(), result.ConversationID, "friend.request.accepted")
			},
		}, nil
	})
}

func (s *Server) handleRejectFriendRequest(w http.ResponseWriter, r *http.Request) {
	if !s.requireFriendService(w, r) {
		return
	}
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}

	requestID := r.PathValue("requestId")
	if reqErr := validateUUID(requestID, "requestId"); reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}

	var req rejectFriendRequestRequest
	if err := decodeOptionalJSONBody(r, &req); err != nil {
		writeRequestError(w, r, err)
		return
	}
	clientOperationID := req.ClientOperationID
	if clientOperationID == "" {
		clientOperationID = r.URL.Query().Get("clientOperationId")
	}

	s.handleIdempotentWrite(w, r, principal.UserID, "friend.request.reject:"+requestID, idempotencyKeyFromRequest(r, clientOperationID), func(err error) {
		writeFriendServiceError(w, r, err)
	}, func() (idempotentHTTPResult, error) {
		request, err := s.friendService.RejectRequest(r.Context(), principal.UserID, requestID)
		if err != nil {
			return idempotentHTTPResult{}, err
		}
		response := friendRequestDetailResponse{Request: makeFriendRequestResponse(request)}
		return idempotentHTTPResult{
			Status: http.StatusOK,
			Body:   response,
			After: func() {
				s.publishRealtimeEvent(r.Context(), []string{request.FromUser.ID, request.ToUser.ID}, "friend.request.rejected", response)
			},
		}, nil
	})
}

func (s *Server) handleGroups(w http.ResponseWriter, r *http.Request) {
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
	if reqErr := validateOptionalUUID(r.URL.Query().Get("categoryId"), "categoryId"); reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}
	if reqErr := validateOptionalGroupRoleFilter(r.URL.Query().Get("role")); reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}

	groups, total, err := s.repos.Groups.ListForUser(
		r.Context(),
		principal.UserID,
		r.URL.Query().Get("keyword"),
		r.URL.Query().Get("categoryId"),
		r.URL.Query().Get("role"),
		limit,
		offset,
	)
	if err != nil {
		writeRepositoryError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, groupsResponse{Groups: makeGroupResponses(groups), Total: total})
}

func (s *Server) handleGroupByPublicID(w http.ResponseWriter, r *http.Request) {
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}

	groupPublicID := r.URL.Query().Get("groupPublicId")
	if reqErr := validateGroupPublicID(groupPublicID, "groupPublicId"); reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}

	group, err := s.repos.Groups.GetByPublicIDForUser(r.Context(), principal.UserID, groupPublicID)
	if err != nil {
		writeRepositoryError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, groupDetailResponse{Group: makeGroupResponse(group)})
}

func (s *Server) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	if !s.requireGroupService(w, r) {
		return
	}
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}

	var req createGroupRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeRequestError(w, r, err)
		return
	}
	for _, userID := range req.MemberIDs {
		if err := validateUUID(userID, "memberIds"); err != nil {
			writeRequestError(w, r, err)
			return
		}
	}
	if req.AvatarFileID != nil {
		if err := validateOptionalUUID(*req.AvatarFileID, "avatarFileId"); err != nil {
			writeRequestError(w, r, err)
			return
		}
	}

	s.handleIdempotentWrite(w, r, principal.UserID, "group.create", idempotencyKeyFromRequest(r, req.ClientOperationID), func(err error) {
		writeGroupServiceError(w, r, err)
	}, func() (idempotentHTTPResult, error) {
		group, err := s.groupService.CreateGroup(r.Context(), groupsvc.CreateGroupInput{
			UserID:       principal.UserID,
			Name:         req.Name,
			MemberIDs:    req.MemberIDs,
			AvatarFileID: req.AvatarFileID,
		})
		if err != nil {
			return idempotentHTTPResult{}, err
		}
		response := groupDetailResponse{Group: makeGroupResponse(group)}
		return idempotentHTTPResult{
			Status: http.StatusCreated,
			ETag:   response.Group.ETag,
			Body:   response,
			After: func() {
				s.publishGroupRealtimeEvent(r.Context(), group.Group.ID, "group.created", response)
			},
		}, nil
	})
}

func (s *Server) handleGroupDetail(w http.ResponseWriter, r *http.Request) {
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

	group, err := s.repos.Groups.GetForUser(r.Context(), principal.UserID, groupID)
	if err != nil {
		writeRepositoryError(w, r, err)
		return
	}

	response := groupDetailResponse{Group: makeGroupResponse(group)}
	writeJSONWithETag(w, r, http.StatusOK, response.Group.ETag, response)
}

func (s *Server) handlePatchGroup(w http.ResponseWriter, r *http.Request) {
	if !s.requireGroupService(w, r) {
		return
	}
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}

	groupID := r.PathValue("groupId")
	if err := validateUUID(groupID, "groupId"); err != nil {
		writeRequestError(w, r, err)
		return
	}
	var req patchGroupRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeRequestError(w, r, err)
		return
	}
	if req.AvatarFileID.Set && req.AvatarFileID.Value != nil {
		if err := validateOptionalUUID(*req.AvatarFileID.Value, "avatarFileId"); err != nil {
			writeRequestError(w, r, err)
			return
		}
	}
	s.handleIdempotentWrite(w, r, principal.UserID, "group.update:"+groupID, idempotencyKeyFromRequest(r, req.ClientOperationID), func(err error) {
		writeVersionedGroupOperationError(w, r, err)
	}, func() (idempotentHTTPResult, error) {
		current, err := s.repos.Groups.GetForUser(r.Context(), principal.UserID, groupID)
		if err != nil {
			return idempotentHTTPResult{}, err
		}
		if !resourceVersionMatches(r, resourceETag("group", current.Group.ID, current.Group.Version), current.Group.Version, req.ExpectedVersion) {
			return idempotentHTTPResult{}, errResourceVersionConflict
		}
		group, err := s.groupService.UpdateGroup(r.Context(), groupsvc.UpdateGroupInput{
			UserID:          principal.UserID,
			GroupID:         groupID,
			Name:            req.Name,
			Introduction:    req.Introduction,
			Announcement:    req.Announcement,
			AvatarFileIDSet: req.AvatarFileID.Set,
			AvatarFileID:    req.AvatarFileID.Value,
		})
		if err != nil {
			return idempotentHTTPResult{}, err
		}
		changedFields, avatarChanged := groupPatchChangedFields(req, current, group)
		response := groupDetailResponse{
			Group:         makeGroupResponse(group),
			ChangedFields: changedFields,
			AvatarChanged: boolPtr(avatarChanged),
		}
		return idempotentHTTPResult{
			Status: http.StatusOK,
			ETag:   response.Group.ETag,
			Body:   response,
			After: func() {
				s.publishGroupRealtimeEvent(r.Context(), groupID, "group.updated", response)
			},
		}, nil
	})
}

func (s *Server) handleDeleteGroup(w http.ResponseWriter, r *http.Request) {
	if !s.requireGroupService(w, r) {
		return
	}
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	groupID := r.PathValue("groupId")
	if err := validateUUID(groupID, "groupId"); err != nil {
		writeRequestError(w, r, err)
		return
	}
	expectedVersion, reqErr := expectedVersionFromQuery(r)
	if reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}
	s.handleIdempotentWrite(w, r, principal.UserID, "group.delete:"+groupID, idempotencyKeyFromRequest(r, r.URL.Query().Get("clientOperationId")), func(err error) {
		writeVersionedGroupOperationError(w, r, err)
	}, func() (idempotentHTTPResult, error) {
		current, err := s.repos.Groups.GetForUser(r.Context(), principal.UserID, groupID)
		if err != nil {
			return idempotentHTTPResult{}, err
		}
		if !resourceVersionMatches(r, resourceETag("group", current.Group.ID, current.Group.Version), current.Group.Version, expectedVersion) {
			return idempotentHTTPResult{}, errResourceVersionConflict
		}
		recipients := []string{}
		action := "group.member.removed"
		if s.repos.Groups != nil {
			if userIDs, err := s.repos.Groups.ListActiveMemberUserIDs(r.Context(), groupID); err == nil {
				recipients = userIDs
			} else {
				s.logger.WarnContext(r.Context(), "failed to list group delete realtime recipients", "error", err, "group_id", groupID)
			}
			if member, err := s.repos.Groups.GetMember(r.Context(), groupID, principal.UserID); err == nil && member.Role == "owner" {
				action = "group.deleted"
			}
		}
		if err := s.groupService.DeleteOrLeaveGroup(r.Context(), principal.UserID, groupID); err != nil {
			return idempotentHTTPResult{}, err
		}
		payload := map[string]any{
			"groupId":      groupID,
			"operatorUuid": principal.UserID,
		}
		return idempotentHTTPResult{
			Status: http.StatusOK,
			Body:   okResponse{OK: true},
			After: func() {
				s.publishGroupRealtimeEventTo(r.Context(), recipients, action, payload)
			},
		}, nil
	})
}

func (s *Server) handleGroupMembers(w http.ResponseWriter, r *http.Request) {
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
	groupID := r.PathValue("groupId")
	if reqErr := validateUUID(groupID, "groupId"); reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}

	if _, err := s.repos.Groups.GetForUser(r.Context(), principal.UserID, groupID); err != nil {
		writeRepositoryError(w, r, err)
		return
	}

	members, total, err := s.repos.Groups.ListMembers(
		r.Context(),
		principal.UserID,
		groupID,
		r.URL.Query().Get("keyword"),
		limit,
		offset,
	)
	if err != nil {
		writeRepositoryError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, groupMembersResponse{Members: s.makeGroupMemberResponsesForViewer(principal.UserID, members), Total: total})
}

func (s *Server) handleGroupMembersBatch(w http.ResponseWriter, r *http.Request) {
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
	var req batchGroupMembersRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeRequestError(w, r, err)
		return
	}
	lookups, reqErr := collectBatchGroupMemberLookups(req)
	if reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}
	if _, err := s.repos.Groups.GetForUser(r.Context(), principal.UserID, groupID); err != nil {
		writeRepositoryError(w, r, err)
		return
	}

	userIDs := make([]string, 0, len(lookups))
	for _, lookup := range lookups {
		userIDs = append(userIDs, lookup.UserUUID)
	}
	members, err := s.repos.Groups.BatchGetMembers(r.Context(), principal.UserID, groupID, userIDs)
	if err != nil {
		writeRepositoryError(w, r, err)
		return
	}

	membersByUserID := make(map[string]repository.GroupMember, len(members))
	for _, member := range members {
		membersByUserID[member.User.ID] = member
	}
	response := batchGroupMembersResponse{Members: []groupMemberResponse{}}
	for _, lookup := range lookups {
		member, ok := membersByUserID[lookup.UserUUID]
		if !ok {
			response.NotFound = append(response.NotFound, lookup.UserUUID)
			continue
		}
		if lookup.HasKnownVersion && member.Version <= lookup.KnownVersion {
			response.NotModified = append(response.NotModified, lookup.UserUUID)
			continue
		}
		response.Members = append(response.Members, s.makeGroupMemberResponseForViewer(principal.UserID, member))
	}

	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleAddGroupMembers(w http.ResponseWriter, r *http.Request) {
	if !s.requireGroupService(w, r) {
		return
	}
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	groupID := r.PathValue("groupId")
	if err := validateUUID(groupID, "groupId"); err != nil {
		writeRequestError(w, r, err)
		return
	}
	var req addGroupMembersRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeRequestError(w, r, err)
		return
	}
	for _, userID := range req.UserUUIDs {
		if err := validateUUID(userID, "userUuids"); err != nil {
			writeRequestError(w, r, err)
			return
		}
	}

	s.handleIdempotentWrite(w, r, principal.UserID, "group.members.add:"+groupID, idempotencyKeyFromRequest(r, req.ClientOperationID), func(err error) {
		writeGroupServiceError(w, r, err)
	}, func() (idempotentHTTPResult, error) {
		members, err := s.groupService.AddMembers(r.Context(), groupsvc.AddMembersInput{
			UserID:  principal.UserID,
			GroupID: groupID,
			UserIDs: req.UserUUIDs,
		})
		if err != nil {
			return idempotentHTTPResult{}, err
		}
		response := groupMembersResponse{Members: s.makeGroupMemberResponsesForViewer(principal.UserID, members), Total: len(members)}
		return idempotentHTTPResult{
			Status: http.StatusCreated,
			Body:   response,
			After: func() {
				eventResponse := groupMembersResponse{Members: makeGroupMemberResponses(members), Total: len(members)}
				s.publishGroupRealtimeEvent(r.Context(), groupID, "group.members.added", eventResponse)
			},
		}, nil
	})
}

func (s *Server) handleRemoveGroupMember(w http.ResponseWriter, r *http.Request) {
	if !s.requireGroupService(w, r) {
		return
	}
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	groupID := r.PathValue("groupId")
	userID := r.PathValue("userUuid")
	if err := validateUUID(groupID, "groupId"); err != nil {
		writeRequestError(w, r, err)
		return
	}
	if err := validateUUID(userID, "userUuid"); err != nil {
		writeRequestError(w, r, err)
		return
	}

	s.handleIdempotentWrite(w, r, principal.UserID, "group.member.remove:"+groupID+":"+userID, idempotencyKeyFromRequest(r, r.URL.Query().Get("clientOperationId")), func(err error) {
		writeGroupServiceError(w, r, err)
	}, func() (idempotentHTTPResult, error) {
		recipients := []string{}
		if s.repos.Groups != nil {
			if userIDs, err := s.repos.Groups.ListActiveMemberUserIDs(r.Context(), groupID); err == nil {
				recipients = userIDs
			} else {
				s.logger.WarnContext(r.Context(), "failed to list group member remove realtime recipients", "error", err, "group_id", groupID)
			}
		}
		if err := s.groupService.RemoveMember(r.Context(), groupsvc.RemoveMemberInput{
			UserID:       principal.UserID,
			GroupID:      groupID,
			TargetUserID: userID,
		}); err != nil {
			return idempotentHTTPResult{}, err
		}
		payload := map[string]any{
			"groupId":      groupID,
			"userUuid":     userID,
			"operatorUuid": principal.UserID,
		}
		return idempotentHTTPResult{
			Status: http.StatusOK,
			Body:   okResponse{OK: true},
			After: func() {
				s.publishGroupRealtimeEventTo(r.Context(), recipients, "group.member.removed", payload)
			},
		}, nil
	})
}

func (s *Server) handlePatchGroupMember(w http.ResponseWriter, r *http.Request) {
	if !s.requireGroupService(w, r) {
		return
	}
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	groupID := r.PathValue("groupId")
	userID := r.PathValue("userUuid")
	if err := validateUUID(groupID, "groupId"); err != nil {
		writeRequestError(w, r, err)
		return
	}
	if err := validateUUID(userID, "userUuid"); err != nil {
		writeRequestError(w, r, err)
		return
	}
	var req patchGroupMemberRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeRequestError(w, r, err)
		return
	}

	s.handleIdempotentWrite(w, r, principal.UserID, "group.member.update:"+groupID+":"+userID, idempotencyKeyFromRequest(r, req.ClientOperationID), func(err error) {
		writeGroupServiceError(w, r, err)
	}, func() (idempotentHTTPResult, error) {
		member, err := s.groupService.UpdateMember(r.Context(), groupsvc.UpdateMemberInput{
			UserID:       principal.UserID,
			GroupID:      groupID,
			TargetUserID: userID,
			Role:         req.Role,
			Nickname:     req.Nickname,
			NicknameSet:  req.Nickname != nil,
			IsDND:        req.IsDND,
		})
		if err != nil {
			return idempotentHTTPResult{}, err
		}
		response := groupMemberDetailResponse{Member: s.makeGroupMemberResponseForViewer(principal.UserID, member)}
		return idempotentHTTPResult{
			Status: http.StatusOK,
			Body:   response,
			After: func() {
				eventResponse := groupMemberDetailResponse{Member: makeGroupMemberResponse(member)}
				s.publishGroupRealtimeEvent(r.Context(), groupID, "group.member.updated", eventResponse)
			},
		}, nil
	})
}

func (s *Server) handleTransferGroupOwner(w http.ResponseWriter, r *http.Request) {
	if !s.requireGroupService(w, r) {
		return
	}
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	groupID := r.PathValue("groupId")
	if err := validateUUID(groupID, "groupId"); err != nil {
		writeRequestError(w, r, err)
		return
	}
	var req transferGroupOwnerRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeRequestError(w, r, err)
		return
	}
	if err := validateUUID(req.UserUUID, "userUuid"); err != nil {
		writeRequestError(w, r, err)
		return
	}

	s.handleIdempotentWrite(w, r, principal.UserID, "group.owner.transfer:"+groupID, idempotencyKeyFromRequest(r, req.ClientOperationID), func(err error) {
		writeGroupServiceError(w, r, err)
	}, func() (idempotentHTTPResult, error) {
		group, err := s.groupService.TransferOwner(r.Context(), groupsvc.TransferOwnerInput{
			UserID:       principal.UserID,
			GroupID:      groupID,
			TargetUserID: req.UserUUID,
		})
		if err != nil {
			return idempotentHTTPResult{}, err
		}
		response := groupDetailResponse{Group: makeGroupResponse(group)}
		return idempotentHTTPResult{
			Status: http.StatusOK,
			ETag:   response.Group.ETag,
			Body:   response,
			After: func() {
				s.publishGroupRealtimeEvent(r.Context(), groupID, "group.owner.transferred", response)
			},
		}, nil
	})
}

func (s *Server) handlePatchGroupMySettings(w http.ResponseWriter, r *http.Request) {
	if !s.requireGroupService(w, r) {
		return
	}
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	groupID := r.PathValue("groupId")
	if err := validateUUID(groupID, "groupId"); err != nil {
		writeRequestError(w, r, err)
		return
	}
	var req patchGroupMySettingsRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeRequestError(w, r, err)
		return
	}
	if req.ListGroupID != nil {
		if err := validateOptionalUUID(*req.ListGroupID, "listGroupId"); err != nil {
			writeRequestError(w, r, err)
			return
		}
	}

	s.handleIdempotentWrite(w, r, principal.UserID, "group.my_settings.update:"+groupID, idempotencyKeyFromRequest(r, req.ClientOperationID), func(err error) {
		writeGroupServiceError(w, r, err)
	}, func() (idempotentHTTPResult, error) {
		settings, err := s.groupService.UpdateMySettings(r.Context(), groupsvc.UpdateMySettingsInput{
			UserID:           principal.UserID,
			GroupID:          groupID,
			Remark:           req.Remark,
			RemarkSet:        req.Remark != nil,
			ListGroupID:      req.ListGroupID,
			ListGroupIDSet:   req.ListGroupID != nil,
			ListGroupName:    req.ListGroupName,
			ListGroupNameSet: req.ListGroupName != nil,
			IsDND:            req.IsDND,
		})
		if err != nil {
			return idempotentHTTPResult{}, err
		}
		response := groupSettingsDetailResponse{Settings: makeGroupSettingsResponse(settings)}
		return idempotentHTTPResult{
			Status: http.StatusOK,
			Body:   response,
			After: func() {
				s.publishSelfRealtimeEvent(r.Context(), principal.UserID, "group.my_settings.updated", response)
			},
		}, nil
	})
}

func (s *Server) handleCreateGroupJoinRequest(w http.ResponseWriter, r *http.Request) {
	if !s.requireGroupService(w, r) {
		return
	}
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	var req createGroupJoinRequestRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeRequestError(w, r, err)
		return
	}

	groupID, reqErr, repoErr := s.resolveGroupJoinRequestGroupID(r.Context(), req)
	if reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}
	if repoErr != nil {
		writeRepositoryError(w, r, repoErr)
		return
	}

	s.handleIdempotentWrite(w, r, principal.UserID, "group.join_request.create:"+groupID, idempotencyKeyFromRequest(r, req.ClientOperationID), func(err error) {
		writeGroupServiceError(w, r, err)
	}, func() (idempotentHTTPResult, error) {
		request, err := s.groupService.CreateJoinRequest(r.Context(), groupsvc.CreateJoinRequestInput{
			UserID:  principal.UserID,
			GroupID: groupID,
			Message: req.Message,
		})
		if err != nil {
			return idempotentHTTPResult{}, err
		}
		response := groupJoinRequestDetailResponse{Request: makeGroupJoinRequestResponse(request)}
		return idempotentHTTPResult{
			Status: http.StatusCreated,
			Body:   response,
			After: func() {
				recipients := []string{request.ActorUser.ID}
				if s.repos.Groups != nil {
					if userIDs, err := s.repos.Groups.ListPrivilegedMemberUserIDs(r.Context(), request.Group.ID); err == nil {
						recipients = append(recipients, userIDs...)
					} else {
						s.logger.WarnContext(r.Context(), "failed to list group join request realtime recipients", "error", err, "group_id", request.Group.ID)
					}
				}
				s.publishGroupRealtimeEventTo(r.Context(), recipients, "group.join_request.created", response)
				s.publishGroupRealtimeEventTo(r.Context(), recipients, "group.notification.created", map[string]any{
					"notification": response.Request,
					"request":      response.Request,
				})
			},
		}, nil
	})
}

func (s *Server) resolveGroupJoinRequestGroupID(ctx context.Context, req createGroupJoinRequestRequest) (string, *requestError, error) {
	groupID := strings.TrimSpace(req.GroupID)
	groupPublicID := strings.TrimSpace(req.GroupPublicID)
	if groupID == "" && groupPublicID == "" {
		return "", &requestError{
			Status:  http.StatusBadRequest,
			Code:    "VALIDATION_ERROR",
			Message: "groupId or groupPublicId is required",
		}, nil
	}
	if groupID != "" && groupPublicID != "" {
		return "", &requestError{
			Status:  http.StatusBadRequest,
			Code:    "VALIDATION_ERROR",
			Message: "use either groupId or groupPublicId, not both",
		}, nil
	}
	if groupID != "" {
		if err := validateUUID(groupID, "groupId"); err != nil {
			return "", err, nil
		}
		return groupID, nil, nil
	}
	if err := validateGroupPublicID(groupPublicID, "groupPublicId"); err != nil {
		return "", err, nil
	}
	group, err := s.repos.Groups.GetByPublicID(ctx, groupPublicID)
	if err != nil {
		return "", nil, err
	}
	return group.ID, nil, nil
}

func (s *Server) handleGroupNotifications(w http.ResponseWriter, r *http.Request) {
	if !s.requireGroupService(w, r) {
		return
	}
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	limit, offset, err := parsePagination(r)
	if err != nil {
		writeRequestError(w, r, err)
		return
	}

	requests, total, unread, svcErr := s.groupService.ListJoinRequests(r.Context(), groupsvc.ListJoinRequestsInput{
		UserID: principal.UserID,
		Limit:  limit,
		Offset: offset,
	})
	if svcErr != nil {
		writeGroupServiceError(w, r, svcErr)
		return
	}

	writeJSON(w, http.StatusOK, groupJoinRequestsResponse{
		Notifications: makeGroupJoinRequestResponses(requests),
		Total:         total,
		UnreadCount:   unread,
	})
}

func (s *Server) handleAcceptGroupJoinRequest(w http.ResponseWriter, r *http.Request) {
	if !s.requireGroupService(w, r) {
		return
	}
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	requestID := r.PathValue("requestId")
	if err := validateUUID(requestID, "requestId"); err != nil {
		writeRequestError(w, r, err)
		return
	}

	s.handleIdempotentWrite(w, r, principal.UserID, "group.join_request.accept:"+requestID, idempotencyKeyFromRequest(r, r.URL.Query().Get("clientOperationId")), func(err error) {
		writeGroupServiceError(w, r, err)
	}, func() (idempotentHTTPResult, error) {
		request, err := s.groupService.AcceptJoinRequest(r.Context(), principal.UserID, requestID)
		if err != nil {
			return idempotentHTTPResult{}, err
		}
		response := groupJoinRequestDetailResponse{Request: makeGroupJoinRequestResponse(request)}
		recipients := []string{request.ActorUser.ID, principal.UserID}
		if s.repos.Groups != nil {
			if userIDs, err := s.repos.Groups.ListActiveMemberUserIDs(r.Context(), request.Group.ID); err == nil {
				recipients = append(recipients, userIDs...)
			} else {
				s.logger.WarnContext(r.Context(), "failed to list group join accept realtime recipients", "error", err, "group_id", request.Group.ID)
			}
		}
		return idempotentHTTPResult{
			Status: http.StatusOK,
			Body:   response,
			After: func() {
				s.publishGroupRealtimeEventTo(r.Context(), recipients, "group.join_request.accepted", response)
			},
		}, nil
	})
}

func (s *Server) handleRejectGroupJoinRequest(w http.ResponseWriter, r *http.Request) {
	if !s.requireGroupService(w, r) {
		return
	}
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	requestID := r.PathValue("requestId")
	if err := validateUUID(requestID, "requestId"); err != nil {
		writeRequestError(w, r, err)
		return
	}

	s.handleIdempotentWrite(w, r, principal.UserID, "group.join_request.reject:"+requestID, idempotencyKeyFromRequest(r, r.URL.Query().Get("clientOperationId")), func(err error) {
		writeGroupServiceError(w, r, err)
	}, func() (idempotentHTTPResult, error) {
		request, err := s.groupService.RejectJoinRequest(r.Context(), principal.UserID, requestID)
		if err != nil {
			return idempotentHTTPResult{}, err
		}
		response := groupJoinRequestDetailResponse{Request: makeGroupJoinRequestResponse(request)}
		recipients := []string{request.ActorUser.ID, principal.UserID}
		if s.repos.Groups != nil {
			if userIDs, err := s.repos.Groups.ListPrivilegedMemberUserIDs(r.Context(), request.Group.ID); err == nil {
				recipients = append(recipients, userIDs...)
			} else {
				s.logger.WarnContext(r.Context(), "failed to list group join reject realtime recipients", "error", err, "group_id", request.Group.ID)
			}
		}
		return idempotentHTTPResult{
			Status: http.StatusOK,
			Body:   response,
			After: func() {
				s.publishGroupRealtimeEventTo(r.Context(), recipients, "group.join_request.rejected", response)
			},
		}, nil
	})
}

func (s *Server) makeUserDetailResponse(ctx context.Context, viewerUserID string, user repository.User) (userDetailResponse, error) {
	response := userDetailResponse{User: s.makePublicUserResponseForViewer(viewerUserID, user)}
	if user.ID == viewerUserID {
		return response, nil
	}

	relation, err := s.repos.Friends.GetRelation(ctx, viewerUserID, user.ID)
	if err == nil {
		response.Relation = &friendRelationResponse{
			IsFriend:        true,
			Remark:          relation.Remark,
			FriendGroupID:   relation.FriendGroupID,
			FriendGroupName: relation.FriendGroupName,
			IsDND:           relation.IsDND,
			Version:         relation.Version,
		}
		return response, nil
	}
	if !errors.Is(err, repository.ErrNotFound) {
		return userDetailResponse{}, err
	}

	response.Relation = &friendRelationResponse{IsFriend: false}
	return response, nil
}

func (s *Server) makeUserResponseForViewer(viewerUserID string, user repository.User) userResponse {
	response := makeUserResponse(user)
	s.attachPresenceForViewer(viewerUserID, &response)
	return response
}

func (s *Server) makePublicUserResponseForViewer(viewerUserID string, user repository.User) userResponse {
	response := makePublicUserResponse(user)
	s.attachPresenceForViewer(viewerUserID, &response)
	return response
}

func (s *Server) makePublicUserResponsesForViewer(viewerUserID string, users []repository.User) []userResponse {
	responses := make([]userResponse, 0, len(users))
	for _, user := range users {
		responses = append(responses, s.makePublicUserResponseForViewer(viewerUserID, user))
	}
	return responses
}

func (s *Server) attachPresenceForViewer(viewerUserID string, response *userResponse) {
	if response == nil {
		return
	}
	response.Presence = s.presenceForViewer(viewerUserID, response.UserUUID, response.LastSeenAt)
}

func (s *Server) presenceForViewer(viewerUserID string, userID string, userLastSeenAt *time.Time) *presenceResponse {
	status := "offline"
	lastSeenAt := userLastSeenAt
	var updatedAt *time.Time
	if s.realtimeHub != nil {
		if state, ok := s.realtimeHub.Presence(userID); ok {
			status = state.Status
			seen := state.LastSeenAt
			lastSeenAt = &seen
			updated := state.UpdatedAt
			updatedAt = &updated
		}
	}
	if viewerUserID != userID {
		status = externallyVisiblePresenceStatus(status)
		if status == "offline" {
			lastSeenAt = userLastSeenAt
			updatedAt = nil
		}
	}
	return &presenceResponse{
		Status:     status,
		LastSeenAt: lastSeenAt,
		UpdatedAt:  updatedAt,
	}
}

func makePublicUserResponse(user repository.User) userResponse {
	response := makeUserResponse(user)
	response.Email = ""
	return response
}

func makePublicUserResponses(users []repository.User) []userResponse {
	responses := make([]userResponse, 0, len(users))
	for _, user := range users {
		responses = append(responses, makePublicUserResponse(user))
	}
	return responses
}

func makeFriendGroupResponses(groups []repository.FriendGroup) []friendGroupResponse {
	responses := make([]friendGroupResponse, 0, len(groups))
	for _, group := range groups {
		responses = append(responses, makeFriendGroupResponse(group))
	}
	return responses
}

func makeFriendGroupResponse(group repository.FriendGroup) friendGroupResponse {
	return friendGroupResponse{
		GroupID:       group.ID,
		FriendGroupID: group.ID,
		Name:          group.Name,
		SortOrder:     group.SortOrder,
		CreatedAt:     group.CreatedAt,
		UpdatedAt:     group.UpdatedAt,
	}
}

func makeFriendResponses(friends []repository.Friend) []friendResponse {
	responses := make([]friendResponse, 0, len(friends))
	for _, friend := range friends {
		responses = append(responses, makeFriendResponse(friend))
	}
	return responses
}

func (s *Server) makeFriendResponsesForViewer(viewerUserID string, friends []repository.Friend) []friendResponse {
	responses := make([]friendResponse, 0, len(friends))
	for _, friend := range friends {
		responses = append(responses, s.makeFriendResponseForViewer(viewerUserID, friend))
	}
	return responses
}

func makeFriendResponse(friend repository.Friend) friendResponse {
	return friendResponse{
		User:            makePublicUserResponse(friend.User),
		Remark:          friend.Remark,
		FriendGroupID:   friend.FriendGroupID,
		FriendGroupName: friend.FriendGroupName,
		IsDND:           friend.IsDND,
		Version:         friend.Version,
		CreatedAt:       friend.CreatedAt,
		UpdatedAt:       friend.UpdatedAt,
	}
}

func (s *Server) makeFriendResponseForViewer(viewerUserID string, friend repository.Friend) friendResponse {
	response := makeFriendResponse(friend)
	s.attachPresenceForViewer(viewerUserID, &response.User)
	return response
}

func makeFriendRequestResponses(requests []repository.FriendRequest) []friendRequestResponse {
	responses := make([]friendRequestResponse, 0, len(requests))
	for _, request := range requests {
		responses = append(responses, makeFriendRequestResponse(request))
	}
	return responses
}

func makeFriendRequestResponse(request repository.FriendRequest) friendRequestResponse {
	return friendRequestResponse{
		RequestID:      request.ID,
		FromUser:       makePublicUserResponse(request.FromUser),
		ToUser:         makePublicUserResponse(request.ToUser),
		Message:        request.Message,
		SourceType:     request.SourceType,
		SourceGroupID:  request.SourceGroupID,
		SourceFriendID: request.SourceFriendID,
		Status:         request.Status,
		HandledAt:      request.HandledAt,
		CreatedAt:      request.CreatedAt,
		UpdatedAt:      request.UpdatedAt,
	}
}

func makeGroupResponses(groups []repository.GroupListEntry) []groupResponse {
	responses := make([]groupResponse, 0, len(groups))
	for _, group := range groups {
		responses = append(responses, makeGroupResponse(group))
	}
	return responses
}

func makeGroupResponse(entry repository.GroupListEntry) groupResponse {
	return groupResponse{
		GroupID:           entry.Group.ID,
		GroupPublicID:     entry.Group.PublicID,
		Name:              entry.Group.Name,
		OwnerUUID:         entry.Group.OwnerID,
		AvatarURL:         avatarContentURL(entry.Group.AvatarFileID),
		AvatarVersion:     entry.Group.AvatarVersion,
		AvatarEtag:        nil,
		AvatarContentHash: nil,
		Introduction:      entry.Group.Introduction,
		Announcement:      entry.Group.Announcement,
		MemberCount:       entry.MemberCount,
		Role:              entry.Role,
		Nickname:          entry.Nickname,
		IsDND:             entry.IsDND,
		Remark:            entry.Remark,
		ListGroupID:       entry.ListGroupID,
		ListGroupName:     entry.ListGroupName,
		Version:           entry.Group.Version,
		ETag:              resourceETag("group", entry.Group.ID, entry.Group.Version),
		CreatedAt:         entry.Group.CreatedAt,
		UpdatedAt:         entry.Group.UpdatedAt,
	}
}

func makeGroupMemberResponses(members []repository.GroupMember) []groupMemberResponse {
	responses := make([]groupMemberResponse, 0, len(members))
	for _, member := range members {
		responses = append(responses, makeGroupMemberResponse(member))
	}
	return responses
}

func (s *Server) makeGroupMemberResponsesForViewer(viewerUserID string, members []repository.GroupMember) []groupMemberResponse {
	responses := make([]groupMemberResponse, 0, len(members))
	for _, member := range members {
		responses = append(responses, s.makeGroupMemberResponseForViewer(viewerUserID, member))
	}
	return responses
}

func makeGroupMemberResponse(member repository.GroupMember) groupMemberResponse {
	return groupMemberResponse{
		User:     makePublicUserResponse(member.User),
		Role:     member.Role,
		Nickname: member.Nickname,
		IsDND:    member.IsDND,
		JoinedAt: member.JoinedAt,
		Version:  member.Version,
	}
}

func (s *Server) makeGroupMemberResponseForViewer(viewerUserID string, member repository.GroupMember) groupMemberResponse {
	response := makeGroupMemberResponse(member)
	s.attachPresenceForViewer(viewerUserID, &response.User)
	return response
}

func makeGroupSettingsResponse(settings repository.GroupUserSettings) groupSettingsResponse {
	return groupSettingsResponse{
		GroupID:       settings.GroupID,
		Remark:        settings.Remark,
		ListGroupID:   settings.ListGroupID,
		ListGroupName: settings.ListGroupName,
		IsDND:         settings.IsDND,
		CreatedAt:     settings.CreatedAt,
		UpdatedAt:     settings.UpdatedAt,
	}
}

func makeGroupJoinRequestResponses(requests []repository.GroupJoinRequest) []groupJoinRequestResponse {
	responses := make([]groupJoinRequestResponse, 0, len(requests))
	for _, request := range requests {
		responses = append(responses, makeGroupJoinRequestResponse(request))
	}
	return responses
}

func makeGroupJoinRequestResponse(request repository.GroupJoinRequest) groupJoinRequestResponse {
	response := groupJoinRequestResponse{
		RequestID: request.ID,
		Group: makeGroupResponse(repository.GroupListEntry{
			Group:       request.Group,
			MemberCount: 0,
		}),
		ActorUser: makePublicUserResponse(request.ActorUser),
		Message:   request.Message,
		Status:    request.Status,
		HandledAt: request.HandledAt,
		CreatedAt: request.CreatedAt,
		UpdatedAt: request.UpdatedAt,
	}
	if request.OperatorUser != nil {
		operator := makePublicUserResponse(*request.OperatorUser)
		response.OperatorUser = &operator
	}
	return response
}

func (s *Server) requireFriendService(w http.ResponseWriter, r *http.Request) bool {
	if s.friendService != nil {
		return true
	}
	writeError(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "Friend service is not configured")
	return false
}

func (s *Server) requireGroupService(w http.ResponseWriter, r *http.Request) bool {
	if s.groupService != nil {
		return true
	}
	writeError(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "Group service is not configured")
	return false
}

func writeFriendServiceError(w http.ResponseWriter, r *http.Request, err error) {
	var requestActionErr *friendsvc.RequestActionError
	if errors.As(err, &requestActionErr) {
		details := friendRequestActionErrorDetails{
			RequestedAction: requestActionErr.Action,
			ActorRole:       requestActionErr.ActorRole,
			CurrentStatus:   requestActionErr.Request.Status,
			Request:         makeFriendRequestResponse(requestActionErr.Request),
		}
		switch {
		case errors.Is(err, friendsvc.ErrRequestAlreadyHandled):
			writeErrorDetailsCause(w, r, http.StatusConflict, "FRIEND_REQUEST_ALREADY_HANDLED", "Friend request is already handled", details, err)
		case errors.Is(err, friendsvc.ErrRequestNotRecipient):
			writeErrorDetailsCause(w, r, http.StatusForbidden, "FRIEND_REQUEST_NOT_RECIPIENT", "Only the recipient can accept or reject this friend request", details, err)
		default:
			writeErrorDetailsCause(w, r, http.StatusConflict, "FRIEND_REQUEST_STATE_CHANGED", "Friend request state changed", details, err)
		}
		return
	}

	switch {
	case errors.Is(err, friendsvc.ErrValidation):
		writeErrorCause(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "Invalid friend request", err)
	case errors.Is(err, friendsvc.ErrConflict):
		writeErrorCause(w, r, http.StatusConflict, "CONFLICT", "Friend resource conflict", err)
	case errors.Is(err, friendsvc.ErrForbidden):
		writeErrorCause(w, r, http.StatusForbidden, "FORBIDDEN", "Forbidden", err)
	case errors.Is(err, friendsvc.ErrUnavailable):
		writeErrorCause(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "Friend service is not ready", err)
	case errors.Is(err, repository.ErrNotFound):
		writeErrorCause(w, r, http.StatusNotFound, "NOT_FOUND", "Resource not found", err)
	default:
		writeErrorCause(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Internal server error", err)
	}
}

func writeGroupServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, groupsvc.ErrValidation):
		writeErrorCause(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "Invalid group request", err)
	case errors.Is(err, groupsvc.ErrConflict):
		writeErrorCause(w, r, http.StatusConflict, "CONFLICT", "Group resource conflict", err)
	case errors.Is(err, groupsvc.ErrForbidden):
		writeErrorCause(w, r, http.StatusForbidden, "FORBIDDEN", "Forbidden", err)
	case errors.Is(err, groupsvc.ErrUnavailable):
		writeErrorCause(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "Group service is not ready", err)
	case errors.Is(err, repository.ErrNotFound):
		writeErrorCause(w, r, http.StatusNotFound, "NOT_FOUND", "Resource not found", err)
	default:
		writeErrorCause(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Internal server error", err)
	}
}

func writeVersionedGroupOperationError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, errResourceVersionConflict) {
		writeErrorCause(w, r, http.StatusConflict, "VERSION_CONFLICT", "Resource version conflict", err)
		return
	}
	writeGroupServiceError(w, r, err)
}

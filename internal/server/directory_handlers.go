package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	friendsvc "nlserver/internal/friend"
	groupsvc "nlserver/internal/group"
	"nlserver/internal/repository"
)

const maxBatchUsers = 100

type batchUsersRequest struct {
	UserUUIDs []string `json:"userUuids"`
}

type createFriendGroupRequest struct {
	Name      string `json:"name"`
	SortOrder int    `json:"sortOrder"`
}

type patchFriendRequest struct {
	Remark  *string `json:"remark"`
	GroupID *string `json:"groupId"`
	IsDND   *bool   `json:"isDnd"`
}

type createFriendRequestRequest struct {
	ToUserUUID       string  `json:"toUserUuid"`
	Message          *string `json:"message"`
	SourceType       *string `json:"sourceType"`
	SourceGroupID    *string `json:"sourceGroupId"`
	SourceFriendUUID *string `json:"sourceFriendUuid"`
}

type acceptFriendRequestRequest struct {
	Remark  *string `json:"remark"`
	GroupID *string `json:"groupId"`
}

type createGroupRequest struct {
	Name         string   `json:"name"`
	MemberIDs    []string `json:"memberIds"`
	AvatarFileID *string  `json:"avatarFileId"`
}

type patchGroupRequest struct {
	Name            *string `json:"name"`
	Introduction    *string `json:"introduction"`
	Announcement    *string `json:"announcement"`
	AvatarFileID    *string `json:"avatarFileId"`
	ExpectedVersion *int    `json:"expectedVersion"`
}

type addGroupMembersRequest struct {
	UserUUIDs []string `json:"userUuids"`
}

type patchGroupMemberRequest struct {
	Role     *string `json:"role"`
	Nickname *string `json:"nickname"`
	IsDND    *bool   `json:"isDnd"`
}

type transferGroupOwnerRequest struct {
	UserUUID string `json:"userUuid"`
}

type patchGroupMySettingsRequest struct {
	Remark        *string `json:"remark"`
	ListGroupID   *string `json:"listGroupId"`
	ListGroupName *string `json:"listGroupName"`
	IsDND         *bool   `json:"isDnd"`
}

type createGroupJoinRequestRequest struct {
	GroupID string  `json:"groupId"`
	Message *string `json:"message"`
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
	IsFriend      bool    `json:"isFriend"`
	Remark        *string `json:"remark"`
	FriendGroupID *string `json:"friendGroupId"`
	IsDND         bool    `json:"isDnd"`
	Version       int     `json:"version,omitempty"`
}

type batchUsersResponse struct {
	Users []userResponse `json:"users"`
}

type friendGroupsResponse struct {
	Groups []friendGroupResponse `json:"groups"`
}

type friendGroupDetailResponse struct {
	Group friendGroupResponse `json:"group"`
}

type friendGroupResponse struct {
	GroupID   string    `json:"groupId"`
	Name      string    `json:"name"`
	SortOrder int       `json:"sortOrder"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type friendsResponse struct {
	Friends []friendResponse `json:"friends"`
	Total   int              `json:"total"`
}

type friendshipResponse struct {
	Friendship friendResponse `json:"friendship"`
}

type friendResponse struct {
	User          userResponse `json:"user"`
	Remark        *string      `json:"remark"`
	FriendGroupID *string      `json:"friendGroupId"`
	IsDND         bool         `json:"isDnd"`
	Version       int          `json:"version"`
	CreatedAt     time.Time    `json:"createdAt"`
	UpdatedAt     time.Time    `json:"updatedAt"`
}

type friendRequestsResponse struct {
	Requests    []friendRequestResponse `json:"requests"`
	Total       int                     `json:"total"`
	UnreadCount int                     `json:"unreadCount"`
}

type friendRequestDetailResponse struct {
	Request friendRequestResponse `json:"request"`
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
	Group groupResponse `json:"group"`
}

type groupSettingsDetailResponse struct {
	Settings groupSettingsResponse `json:"settings"`
}

type groupResponse struct {
	GroupID           string    `json:"groupId"`
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

func (s *Server) handleUsersSearch(w http.ResponseWriter, r *http.Request) {
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
		Users: makePublicUserResponses(users),
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

	response, err := makeUserDetailResponse(r.Context(), s.repos, principal.UserID, user)
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

	response, err := makeUserDetailResponse(r.Context(), s.repos, principal.UserID, user)
	if err != nil {
		writeRepositoryError(w, r, err)
		return
	}

	writeJSONWithETag(w, r, http.StatusOK, response.User.ETag, response)
}

func (s *Server) handleUsersBatch(w http.ResponseWriter, r *http.Request) {
	var req batchUsersRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeRequestError(w, r, err)
		return
	}
	if len(req.UserUUIDs) > maxBatchUsers {
		writeRequestError(w, r, &requestError{
			Status:  http.StatusBadRequest,
			Code:    "VALIDATION_ERROR",
			Message: "userUuids must contain at most 100 entries",
		})
		return
	}
	for _, userUUID := range req.UserUUIDs {
		if err := validateUUID(userUUID, "userUuids"); err != nil {
			writeRequestError(w, r, err)
			return
		}
	}

	users, err := s.repos.Users.BatchGetByIDs(r.Context(), req.UserUUIDs)
	if err != nil {
		writeRepositoryError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, batchUsersResponse{Users: makePublicUserResponses(users)})
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

	group, err := s.friendService.CreateGroup(r.Context(), friendsvc.CreateGroupInput{
		UserID:    principal.UserID,
		Name:      req.Name,
		SortOrder: req.SortOrder,
	})
	if err != nil {
		writeFriendServiceError(w, r, err)
		return
	}

	response := friendGroupDetailResponse{Group: makeFriendGroupResponse(group)}
	s.publishSelfRealtimeEvent(r.Context(), principal.UserID, "friend.group.created", response)
	writeJSON(w, http.StatusCreated, response)
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

	writeJSON(w, http.StatusOK, friendsResponse{Friends: makeFriendResponses(friends), Total: total})
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
	if req.GroupID != nil {
		if reqErr := validateOptionalUUID(*req.GroupID, "groupId"); reqErr != nil {
			writeRequestError(w, r, reqErr)
			return
		}
	}

	friend, err := s.friendService.UpdateFriend(r.Context(), friendsvc.UpdateFriendInput{
		UserID:       principal.UserID,
		FriendUserID: friendUserID,
		RemarkSet:    req.Remark != nil,
		Remark:       req.Remark,
		GroupIDSet:   req.GroupID != nil,
		GroupID:      req.GroupID,
		IsDND:        req.IsDND,
	})
	if err != nil {
		writeFriendServiceError(w, r, err)
		return
	}

	response := friendshipResponse{Friendship: makeFriendResponse(friend)}
	s.publishSelfRealtimeEvent(r.Context(), principal.UserID, "friend.updated", response)
	writeJSON(w, http.StatusOK, response)
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

	if err := s.friendService.DeleteFriend(r.Context(), principal.UserID, friendUserID); err != nil {
		writeFriendServiceError(w, r, err)
		return
	}

	s.publishRealtimeEvent(r.Context(), []string{principal.UserID, friendUserID}, "friend.deleted", map[string]any{
		"userUuid":       principal.UserID,
		"friendUserUuid": friendUserID,
	})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
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
	if reqErr := validateUUID(req.ToUserUUID, "toUserUuid"); reqErr != nil {
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

	request, err := s.friendService.CreateRequest(r.Context(), friendsvc.CreateRequestInput{
		FromUserID:     principal.UserID,
		ToUserID:       req.ToUserUUID,
		Message:        req.Message,
		SourceType:     req.SourceType,
		SourceGroupID:  req.SourceGroupID,
		SourceFriendID: req.SourceFriendUUID,
	})
	if err != nil {
		writeFriendServiceError(w, r, err)
		return
	}

	response := friendRequestDetailResponse{Request: makeFriendRequestResponse(request)}
	s.publishRealtimeEvent(r.Context(), []string{request.FromUser.ID, request.ToUser.ID}, "friend.request.created", response)
	writeJSON(w, http.StatusCreated, response)
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
	if req.GroupID != nil {
		if reqErr := validateOptionalUUID(*req.GroupID, "groupId"); reqErr != nil {
			writeRequestError(w, r, reqErr)
			return
		}
	}

	friend, err := s.friendService.AcceptRequest(r.Context(), friendsvc.AcceptRequestInput{
		UserID:    principal.UserID,
		RequestID: requestID,
		Remark:    req.Remark,
		GroupID:   req.GroupID,
	})
	if err != nil {
		writeFriendServiceError(w, r, err)
		return
	}

	response := friendshipResponse{Friendship: makeFriendResponse(friend)}
	s.publishRealtimeEvent(r.Context(), []string{principal.UserID, friend.User.ID}, "friend.request.accepted", response)
	writeJSON(w, http.StatusOK, response)
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

	request, err := s.friendService.RejectRequest(r.Context(), principal.UserID, requestID)
	if err != nil {
		writeFriendServiceError(w, r, err)
		return
	}

	response := friendRequestDetailResponse{Request: makeFriendRequestResponse(request)}
	s.publishRealtimeEvent(r.Context(), []string{request.FromUser.ID, request.ToUser.ID}, "friend.request.rejected", response)
	writeJSON(w, http.StatusOK, response)
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

	groups, total, err := s.repos.Groups.ListForUser(
		r.Context(),
		principal.UserID,
		r.URL.Query().Get("keyword"),
		r.URL.Query().Get("categoryId"),
		limit,
		offset,
	)
	if err != nil {
		writeRepositoryError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, groupsResponse{Groups: makeGroupResponses(groups), Total: total})
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

	group, err := s.groupService.CreateGroup(r.Context(), groupsvc.CreateGroupInput{
		UserID:       principal.UserID,
		Name:         req.Name,
		MemberIDs:    req.MemberIDs,
		AvatarFileID: req.AvatarFileID,
	})
	if err != nil {
		writeGroupServiceError(w, r, err)
		return
	}

	response := groupDetailResponse{Group: makeGroupResponse(group)}
	s.publishGroupRealtimeEvent(r.Context(), group.Group.ID, "group.created", response)
	writeJSONWithETag(w, r, http.StatusCreated, response.Group.ETag, response)
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
	if req.AvatarFileID != nil {
		if err := validateOptionalUUID(*req.AvatarFileID, "avatarFileId"); err != nil {
			writeRequestError(w, r, err)
			return
		}
	}
	current, err := s.repos.Groups.GetForUser(r.Context(), principal.UserID, groupID)
	if err != nil {
		writeRepositoryError(w, r, err)
		return
	}
	if !checkResourceVersion(w, r, resourceETag("group", current.Group.ID, current.Group.Version), current.Group.Version, req.ExpectedVersion) {
		return
	}

	group, err := s.groupService.UpdateGroup(r.Context(), groupsvc.UpdateGroupInput{
		UserID:          principal.UserID,
		GroupID:         groupID,
		Name:            req.Name,
		Introduction:    req.Introduction,
		Announcement:    req.Announcement,
		AvatarFileIDSet: req.AvatarFileID != nil,
		AvatarFileID:    req.AvatarFileID,
	})
	if err != nil {
		writeGroupServiceError(w, r, err)
		return
	}

	response := groupDetailResponse{Group: makeGroupResponse(group)}
	s.publishGroupRealtimeEvent(r.Context(), groupID, "group.updated", response)
	writeJSONWithETag(w, r, http.StatusOK, response.Group.ETag, response)
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
	current, err := s.repos.Groups.GetForUser(r.Context(), principal.UserID, groupID)
	if err != nil {
		writeRepositoryError(w, r, err)
		return
	}
	if !checkResourceVersion(w, r, resourceETag("group", current.Group.ID, current.Group.Version), current.Group.Version, expectedVersion) {
		return
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
		writeGroupServiceError(w, r, err)
		return
	}

	s.publishGroupRealtimeEventTo(r.Context(), recipients, action, map[string]any{
		"groupId":      groupID,
		"operatorUuid": principal.UserID,
	})
	writeJSON(w, http.StatusOK, okResponse{OK: true})
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

	writeJSON(w, http.StatusOK, groupMembersResponse{Members: makeGroupMemberResponses(members), Total: total})
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

	members, err := s.groupService.AddMembers(r.Context(), groupsvc.AddMembersInput{
		UserID:  principal.UserID,
		GroupID: groupID,
		UserIDs: req.UserUUIDs,
	})
	if err != nil {
		writeGroupServiceError(w, r, err)
		return
	}

	response := groupMembersResponse{Members: makeGroupMemberResponses(members), Total: len(members)}
	s.publishGroupRealtimeEvent(r.Context(), groupID, "group.members.added", response)
	writeJSON(w, http.StatusCreated, response)
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
		writeGroupServiceError(w, r, err)
		return
	}

	s.publishGroupRealtimeEventTo(r.Context(), recipients, "group.member.removed", map[string]any{
		"groupId":      groupID,
		"userUuid":     userID,
		"operatorUuid": principal.UserID,
	})
	writeJSON(w, http.StatusOK, okResponse{OK: true})
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
		writeGroupServiceError(w, r, err)
		return
	}

	response := groupMemberDetailResponse{Member: makeGroupMemberResponse(member)}
	s.publishGroupRealtimeEvent(r.Context(), groupID, "group.member.updated", response)
	writeJSON(w, http.StatusOK, response)
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

	group, err := s.groupService.TransferOwner(r.Context(), groupsvc.TransferOwnerInput{
		UserID:       principal.UserID,
		GroupID:      groupID,
		TargetUserID: req.UserUUID,
	})
	if err != nil {
		writeGroupServiceError(w, r, err)
		return
	}

	response := groupDetailResponse{Group: makeGroupResponse(group)}
	s.publishGroupRealtimeEvent(r.Context(), groupID, "group.owner.transferred", response)
	writeJSON(w, http.StatusOK, response)
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
		writeGroupServiceError(w, r, err)
		return
	}

	response := groupSettingsDetailResponse{Settings: makeGroupSettingsResponse(settings)}
	s.publishSelfRealtimeEvent(r.Context(), principal.UserID, "group.my_settings.updated", response)
	writeJSON(w, http.StatusOK, response)
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
	if err := validateUUID(req.GroupID, "groupId"); err != nil {
		writeRequestError(w, r, err)
		return
	}

	request, err := s.groupService.CreateJoinRequest(r.Context(), groupsvc.CreateJoinRequestInput{
		UserID:  principal.UserID,
		GroupID: req.GroupID,
		Message: req.Message,
	})
	if err != nil {
		writeGroupServiceError(w, r, err)
		return
	}

	response := groupJoinRequestDetailResponse{Request: makeGroupJoinRequestResponse(request)}
	recipients := []string{request.ActorUser.ID}
	if s.repos.Groups != nil {
		if userIDs, err := s.repos.Groups.ListPrivilegedMemberUserIDs(r.Context(), request.Group.ID); err == nil {
			recipients = append(recipients, userIDs...)
		} else {
			s.logger.WarnContext(r.Context(), "failed to list group join request realtime recipients", "error", err, "group_id", request.Group.ID)
		}
	}
	s.publishGroupRealtimeEventTo(r.Context(), recipients, "group.join_request.created", response)
	writeJSON(w, http.StatusCreated, response)
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

	request, err := s.groupService.AcceptJoinRequest(r.Context(), principal.UserID, requestID)
	if err != nil {
		writeGroupServiceError(w, r, err)
		return
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
	s.publishGroupRealtimeEventTo(r.Context(), recipients, "group.join_request.accepted", response)
	writeJSON(w, http.StatusOK, response)
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

	request, err := s.groupService.RejectJoinRequest(r.Context(), principal.UserID, requestID)
	if err != nil {
		writeGroupServiceError(w, r, err)
		return
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
	s.publishGroupRealtimeEventTo(r.Context(), recipients, "group.join_request.rejected", response)
	writeJSON(w, http.StatusOK, response)
}

func makeUserDetailResponse(ctx context.Context, repos repository.Repositories, viewerUserID string, user repository.User) (userDetailResponse, error) {
	response := userDetailResponse{User: makePublicUserResponse(user)}
	if user.ID == viewerUserID {
		return response, nil
	}

	relation, err := repos.Friends.GetRelation(ctx, viewerUserID, user.ID)
	if err == nil {
		response.Relation = &friendRelationResponse{
			IsFriend:      true,
			Remark:        relation.Remark,
			FriendGroupID: relation.FriendGroupID,
			IsDND:         relation.IsDND,
			Version:       relation.Version,
		}
		return response, nil
	}
	if !errors.Is(err, repository.ErrNotFound) {
		return userDetailResponse{}, err
	}

	response.Relation = &friendRelationResponse{IsFriend: false}
	return response, nil
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
		GroupID:   group.ID,
		Name:      group.Name,
		SortOrder: group.SortOrder,
		CreatedAt: group.CreatedAt,
		UpdatedAt: group.UpdatedAt,
	}
}

func makeFriendResponses(friends []repository.Friend) []friendResponse {
	responses := make([]friendResponse, 0, len(friends))
	for _, friend := range friends {
		responses = append(responses, makeFriendResponse(friend))
	}
	return responses
}

func makeFriendResponse(friend repository.Friend) friendResponse {
	return friendResponse{
		User:          makePublicUserResponse(friend.User),
		Remark:        friend.Remark,
		FriendGroupID: friend.FriendGroupID,
		IsDND:         friend.IsDND,
		Version:       friend.Version,
		CreatedAt:     friend.CreatedAt,
		UpdatedAt:     friend.UpdatedAt,
	}
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
		Name:              entry.Group.Name,
		OwnerUUID:         entry.Group.OwnerID,
		AvatarURL:         nil,
		AvatarVersion:     entry.Group.Version,
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
	switch {
	case errors.Is(err, friendsvc.ErrValidation):
		writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "Invalid friend request")
	case errors.Is(err, friendsvc.ErrConflict):
		writeError(w, r, http.StatusConflict, "CONFLICT", "Friend resource conflict")
	case errors.Is(err, friendsvc.ErrForbidden):
		writeError(w, r, http.StatusForbidden, "FORBIDDEN", "Forbidden")
	case errors.Is(err, friendsvc.ErrUnavailable):
		writeError(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "Friend service is not ready")
	case errors.Is(err, repository.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Resource not found")
	default:
		writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Internal server error")
	}
}

func writeGroupServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, groupsvc.ErrValidation):
		writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "Invalid group request")
	case errors.Is(err, groupsvc.ErrConflict):
		writeError(w, r, http.StatusConflict, "CONFLICT", "Group resource conflict")
	case errors.Is(err, groupsvc.ErrForbidden):
		writeError(w, r, http.StatusForbidden, "FORBIDDEN", "Forbidden")
	case errors.Is(err, groupsvc.ErrUnavailable):
		writeError(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "Group service is not ready")
	case errors.Is(err, repository.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Resource not found")
	default:
		writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Internal server error")
	}
}

package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"nlserver/internal/repository"
)

const maxBatchUsers = 100

type batchUsersRequest struct {
	UserUUIDs []string `json:"userUuids"`
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

type friendResponse struct {
	User          userResponse `json:"user"`
	Remark        *string      `json:"remark"`
	FriendGroupID *string      `json:"friendGroupId"`
	IsDND         bool         `json:"isDnd"`
	Version       int          `json:"version"`
	CreatedAt     time.Time    `json:"createdAt"`
	UpdatedAt     time.Time    `json:"updatedAt"`
}

type groupsResponse struct {
	Groups []groupResponse `json:"groups"`
	Total  int             `json:"total"`
}

type groupDetailResponse struct {
	Group groupResponse `json:"group"`
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
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

type groupMembersResponse struct {
	Members []groupMemberResponse `json:"members"`
	Total   int                   `json:"total"`
}

type groupMemberResponse struct {
	User     userResponse `json:"user"`
	Role     string       `json:"role"`
	Nickname *string      `json:"nickname"`
	IsDND    bool         `json:"isDnd"`
	JoinedAt time.Time    `json:"joinedAt"`
	Version  int          `json:"version"`
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

	writeJSON(w, http.StatusOK, response)
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

	writeJSON(w, http.StatusOK, response)
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

	writeJSON(w, http.StatusOK, groupDetailResponse{Group: makeGroupResponse(group)})
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
		responses = append(responses, friendGroupResponse{
			GroupID:   group.ID,
			Name:      group.Name,
			SortOrder: group.SortOrder,
			CreatedAt: group.CreatedAt,
			UpdatedAt: group.UpdatedAt,
		})
	}
	return responses
}

func makeFriendResponses(friends []repository.Friend) []friendResponse {
	responses := make([]friendResponse, 0, len(friends))
	for _, friend := range friends {
		responses = append(responses, friendResponse{
			User:          makePublicUserResponse(friend.User),
			Remark:        friend.Remark,
			FriendGroupID: friend.FriendGroupID,
			IsDND:         friend.IsDND,
			Version:       friend.Version,
			CreatedAt:     friend.CreatedAt,
			UpdatedAt:     friend.UpdatedAt,
		})
	}
	return responses
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
		CreatedAt:         entry.Group.CreatedAt,
		UpdatedAt:         entry.Group.UpdatedAt,
	}
}

func makeGroupMemberResponses(members []repository.GroupMember) []groupMemberResponse {
	responses := make([]groupMemberResponse, 0, len(members))
	for _, member := range members {
		responses = append(responses, groupMemberResponse{
			User:     makePublicUserResponse(member.User),
			Role:     member.Role,
			Nickname: member.Nickname,
			IsDND:    member.IsDND,
			JoinedAt: member.JoinedAt,
			Version:  member.Version,
		})
	}
	return responses
}

package server

import (
	"context"
	"errors"
	"time"

	"nlserver/internal/repository"
)

type userRefResponse struct {
	UserUUID          string    `json:"userUuid"`
	UserID            string    `json:"userId"`
	Version           int       `json:"version"`
	ETag              string    `json:"etag"`
	AvatarURL         *string   `json:"avatarUrl"`
	AvatarVersion     int       `json:"avatarVersion"`
	AvatarEtag        *string   `json:"avatarEtag"`
	AvatarContentHash *string   `json:"avatarContentHash"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

type groupMemberRefResponse struct {
	GroupID   string    `json:"groupId"`
	UserUUID  string    `json:"userUuid"`
	Version   int       `json:"version"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type referencedResourcesResponse struct {
	Users        []userRefResponse        `json:"users,omitempty"`
	GroupMembers []groupMemberRefResponse `json:"groupMembers,omitempty"`
}

type includedResourcesResponse struct {
	Users        []userResponse        `json:"users,omitempty"`
	GroupMembers []groupMemberResponse `json:"groupMembers,omitempty"`
}

func makeUserRefResponse(user repository.User) userRefResponse {
	return userRefResponse{
		UserUUID:          user.ID,
		UserID:            user.PublicID,
		Version:           user.Version,
		ETag:              resourceETag("user", user.ID, user.Version),
		AvatarURL:         avatarContentURL(user.AvatarFileID),
		AvatarVersion:     user.Version,
		AvatarEtag:        nil,
		AvatarContentHash: nil,
		UpdatedAt:         user.UpdatedAt,
	}
}

func makeUserRefResponses(users []repository.User) []userRefResponse {
	responses := make([]userRefResponse, 0, len(users))
	for _, user := range users {
		responses = append(responses, makeUserRefResponse(user))
	}
	return responses
}

func makeGroupMemberRefResponse(member repository.GroupMember) groupMemberRefResponse {
	return groupMemberRefResponse{
		GroupID:   member.GroupID,
		UserUUID:  member.User.ID,
		Version:   member.Version,
		UpdatedAt: member.UpdatedAt,
	}
}

func (refs referencedResourcesResponse) empty() bool {
	return len(refs.Users) == 0 && len(refs.GroupMembers) == 0
}

func (included includedResourcesResponse) empty() bool {
	return len(included.Users) == 0 && len(included.GroupMembers) == 0
}

func (s *Server) makeUserRefsForIDs(ctx context.Context, userIDs []string) (*referencedResourcesResponse, error) {
	users, err := s.loadUsersForIDs(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	refs := referencedResourcesResponse{Users: makeUserRefResponses(users)}
	if refs.empty() {
		return nil, nil
	}
	return &refs, nil
}

func (s *Server) loadUsersForIDs(ctx context.Context, userIDs []string) ([]repository.User, error) {
	if s.repos.Users == nil {
		return []repository.User{}, nil
	}
	unique := uniqueStrings(userIDs)
	if len(unique) == 0 {
		return []repository.User{}, nil
	}
	return s.repos.Users.BatchGetByIDs(ctx, unique)
}

func (s *Server) makeIncludedForMessages(ctx context.Context, viewerUserID string, conversationID string, messages []repository.Message) (*includedResourcesResponse, error) {
	userIDs := make([]string, 0, len(messages))
	for _, message := range messages {
		userIDs = append(userIDs, message.SenderID)
	}
	userIDs = uniqueStrings(userIDs)
	if len(userIDs) == 0 {
		return nil, nil
	}

	users, err := s.loadUsersForIDs(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	members, err := s.loadGroupMembersForConversation(ctx, viewerUserID, conversationID, userIDs)
	if err != nil {
		return nil, err
	}
	included := includedResourcesResponse{
		Users:        makePublicUserResponses(users),
		GroupMembers: makeGroupMemberResponses(members),
	}
	if included.empty() {
		return nil, nil
	}
	return &included, nil
}

func (s *Server) makeIncludedForConversationEntries(ctx context.Context, viewerUserID string, entries []repository.ConversationEntry) (*includedResourcesResponse, error) {
	userIDs := []string{}
	groupUserIDs := map[string][]string{}
	for _, entry := range entries {
		if entry.PeerUser != nil {
			userIDs = append(userIDs, entry.PeerUser.ID)
		}
		if entry.LastMessage == nil {
			continue
		}
		userIDs = append(userIDs, entry.LastMessage.SenderID)
		if entry.Conversation.GroupID != nil {
			groupID := *entry.Conversation.GroupID
			groupUserIDs[groupID] = append(groupUserIDs[groupID], entry.LastMessage.SenderID)
		}
	}

	users, err := s.loadUsersForIDs(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	members := []repository.GroupMember{}
	if s.repos.Groups != nil {
		seenMembers := map[string]struct{}{}
		for groupID, memberUserIDs := range groupUserIDs {
			groupMembers, err := s.repos.Groups.BatchGetMembers(ctx, viewerUserID, groupID, uniqueStrings(memberUserIDs))
			if err != nil {
				return nil, err
			}
			for _, member := range groupMembers {
				key := member.GroupID + ":" + member.User.ID
				if _, ok := seenMembers[key]; ok {
					continue
				}
				seenMembers[key] = struct{}{}
				members = append(members, member)
			}
		}
	}

	included := includedResourcesResponse{
		Users:        makePublicUserResponses(users),
		GroupMembers: makeGroupMemberResponses(members),
	}
	if included.empty() {
		return nil, nil
	}
	return &included, nil
}

func (s *Server) makeRealtimeMessageRefs(ctx context.Context, conversationID string, message messageResponse) *referencedResourcesResponse {
	refs := referencedResourcesResponse{}
	users, err := s.loadUsersForIDs(ctx, []string{message.SenderUUID})
	if err != nil {
		s.logger.WarnContext(ctx, "failed to load realtime user refs", "error", err, "conversation_id", conversationID, "sender_id", message.SenderUUID)
	} else {
		refs.Users = makeUserRefResponses(users)
	}

	member, err := s.loadGroupMemberForConversationSender(ctx, conversationID, message.SenderUUID)
	if err != nil {
		s.logger.WarnContext(ctx, "failed to load realtime group member refs", "error", err, "conversation_id", conversationID, "sender_id", message.SenderUUID)
	} else if member != nil {
		refs.GroupMembers = []groupMemberRefResponse{makeGroupMemberRefResponse(*member)}
	}

	if refs.empty() {
		return nil
	}
	return &refs
}

func (s *Server) loadGroupMembersForConversation(ctx context.Context, viewerUserID string, conversationID string, userIDs []string) ([]repository.GroupMember, error) {
	if s.repos.Chat == nil || s.repos.Groups == nil || len(userIDs) == 0 {
		return []repository.GroupMember{}, nil
	}
	conversation, err := s.repos.Chat.GetConversationByID(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if conversation.GroupID == nil {
		return []repository.GroupMember{}, nil
	}
	return s.repos.Groups.BatchGetMembers(ctx, viewerUserID, *conversation.GroupID, uniqueStrings(userIDs))
}

func (s *Server) loadGroupMemberForConversationSender(ctx context.Context, conversationID string, senderUserID string) (*repository.GroupMember, error) {
	if s.repos.Chat == nil || s.repos.Groups == nil || senderUserID == "" {
		return nil, nil
	}
	conversation, err := s.repos.Chat.GetConversationByID(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if conversation.GroupID == nil {
		return nil, nil
	}
	member, err := s.repos.Groups.GetMember(ctx, *conversation.GroupID, senderUserID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &member, nil
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	return unique
}

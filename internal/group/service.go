package groups

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"nlserver/internal/repository"
)

var (
	ErrValidation  = errors.New("group: validation failed")
	ErrConflict    = errors.New("group: conflict")
	ErrForbidden   = errors.New("group: forbidden")
	ErrUnavailable = errors.New("group: unavailable")
)

const (
	maxGroupNameLength        = 80
	maxGroupTextLength        = 500
	maxGroupMemberBatch       = 100
	maxGroupNicknameLength    = 64
	maxGroupListNameLength    = 64
	maxGroupJoinMessageLength = 256
	groupRoleOwner            = "owner"
	groupRoleAdmin            = "admin"
	groupRoleMember           = "member"
)

type Service struct {
	repos repository.Repositories
	tx    repository.TxBeginner
}

type CreateGroupInput struct {
	UserID       string
	Name         string
	MemberIDs    []string
	AvatarFileID *string
}

type UpdateGroupInput struct {
	UserID          string
	GroupID         string
	Name            *string
	Introduction    *string
	Announcement    *string
	AvatarFileIDSet bool
	AvatarFileID    *string
}

type AddMembersInput struct {
	UserID  string
	GroupID string
	UserIDs []string
}

type RemoveMemberInput struct {
	UserID       string
	GroupID      string
	TargetUserID string
}

type UpdateMemberInput struct {
	UserID       string
	GroupID      string
	TargetUserID string
	Role         *string
	Nickname     *string
	NicknameSet  bool
	IsDND        *bool
}

type TransferOwnerInput struct {
	UserID       string
	GroupID      string
	TargetUserID string
}

type UpdateMySettingsInput struct {
	UserID           string
	GroupID          string
	Remark           *string
	RemarkSet        bool
	ListGroupID      *string
	ListGroupIDSet   bool
	ListGroupName    *string
	ListGroupNameSet bool
	IsDND            *bool
}

type CreateJoinRequestInput struct {
	UserID  string
	GroupID string
	Message *string
}

type ListJoinRequestsInput struct {
	UserID string
	Limit  int
	Offset int
}

func NewService(repos repository.Repositories, tx repository.TxBeginner) *Service {
	return &Service{repos: repos, tx: tx}
}

func (s *Service) CreateGroup(ctx context.Context, input CreateGroupInput) (repository.GroupListEntry, error) {
	if s.repos.Groups == nil || s.tx == nil {
		return repository.GroupListEntry{}, ErrUnavailable
	}
	if normalizedOptional(input.AvatarFileID) != nil && s.repos.Files == nil {
		return repository.GroupListEntry{}, ErrUnavailable
	}
	name := strings.TrimSpace(input.Name)
	if strings.TrimSpace(input.UserID) == "" || name == "" || len([]rune(name)) > maxGroupNameLength {
		return repository.GroupListEntry{}, ErrValidation
	}
	memberIDs := uniqueUserIDs(append([]string{input.UserID}, input.MemberIDs...))
	if len(memberIDs) > maxGroupMemberBatch {
		return repository.GroupListEntry{}, ErrValidation
	}

	var groupID string
	err := repository.WithTx(ctx, s.tx, func(repos repository.Repositories) error {
		avatarFileID := normalizedOptional(input.AvatarFileID)
		if avatarFileID != nil {
			if err := validateOwnedGroupAvatarFile(ctx, repos, input.UserID, *avatarFileID); err != nil {
				return err
			}
		}
		group, err := repos.Groups.Create(ctx, repository.CreateGroupParams{
			Name:         name,
			OwnerID:      input.UserID,
			AvatarFileID: avatarFileID,
		})
		if err != nil {
			return mapRepositoryError(err)
		}
		groupID = group.ID
		if avatarFileID != nil {
			if _, err := repos.Files.CreateUsage(ctx, repository.CreateFileUsageParams{
				FileID:      *avatarFileID,
				UsageType:   "group_avatar",
				BusinessID:  group.ID,
				OwnerUserID: input.UserID,
				Visibility:  "protected",
			}); err != nil {
				return mapRepositoryError(err)
			}
		}
		if err := repos.Groups.AddMembers(ctx, group.ID, []string{input.UserID}, groupRoleOwner); err != nil {
			return mapRepositoryError(err)
		}
		others := withoutUser(memberIDs, input.UserID)
		if len(others) > 0 {
			if err := repos.Groups.AddMembers(ctx, group.ID, others, groupRoleMember); err != nil {
				return mapRepositoryError(err)
			}
		}
		return repos.Groups.AddEvent(ctx, group.ID, "group.created", &input.UserID, nil, eventPayload("name", name))
	})
	if err != nil {
		return repository.GroupListEntry{}, err
	}

	return s.repos.Groups.GetForUser(ctx, input.UserID, groupID)
}

func (s *Service) UpdateGroup(ctx context.Context, input UpdateGroupInput) (repository.GroupListEntry, error) {
	if s.repos.Groups == nil || s.tx == nil {
		return repository.GroupListEntry{}, ErrUnavailable
	}
	if input.AvatarFileIDSet && s.repos.Files == nil {
		return repository.GroupListEntry{}, ErrUnavailable
	}
	if strings.TrimSpace(input.UserID) == "" || strings.TrimSpace(input.GroupID) == "" {
		return repository.GroupListEntry{}, ErrValidation
	}
	name, err := validateOptionalText(input.Name, maxGroupNameLength, true)
	if err != nil {
		return repository.GroupListEntry{}, err
	}
	introduction, err := validateOptionalText(input.Introduction, maxGroupTextLength, false)
	if err != nil {
		return repository.GroupListEntry{}, err
	}
	announcement, err := validateOptionalText(input.Announcement, maxGroupTextLength, false)
	if err != nil {
		return repository.GroupListEntry{}, err
	}

	member, err := s.repos.Groups.GetMember(ctx, input.GroupID, input.UserID)
	if err != nil {
		return repository.GroupListEntry{}, mapRepositoryError(err)
	}
	if !canManageGroup(member.Role) {
		return repository.GroupListEntry{}, ErrForbidden
	}

	if err := repository.WithTx(ctx, s.tx, func(repos repository.Repositories) error {
		avatarFileID := normalizedOptional(input.AvatarFileID)
		if input.AvatarFileIDSet && avatarFileID != nil {
			if err := validateOwnedGroupAvatarFile(ctx, repos, input.UserID, *avatarFileID); err != nil {
				return err
			}
		}
		if _, err := repos.Groups.Update(ctx, repository.UpdateGroupParams{
			GroupID:         input.GroupID,
			NameSet:         input.Name != nil,
			Name:            name,
			IntroductionSet: input.Introduction != nil,
			Introduction:    introduction,
			AnnouncementSet: input.Announcement != nil,
			Announcement:    announcement,
			AvatarFileIDSet: input.AvatarFileIDSet,
			AvatarFileID:    avatarFileID,
		}); err != nil {
			return mapRepositoryError(err)
		}
		if input.AvatarFileIDSet {
			if _, err := repos.Files.SoftDeleteUsages(ctx, "group_avatar", input.GroupID); err != nil {
				return mapRepositoryError(err)
			}
			if avatarFileID != nil {
				if _, err := repos.Files.CreateUsage(ctx, repository.CreateFileUsageParams{
					FileID:      *avatarFileID,
					UsageType:   "group_avatar",
					BusinessID:  input.GroupID,
					OwnerUserID: input.UserID,
					Visibility:  "protected",
				}); err != nil {
					return mapRepositoryError(err)
				}
			}
		}
		return nil
	}); err != nil {
		return repository.GroupListEntry{}, err
	}

	return s.repos.Groups.GetForUser(ctx, input.UserID, input.GroupID)
}

func (s *Service) DeleteOrLeaveGroup(ctx context.Context, userID string, groupID string) error {
	if s.repos.Groups == nil {
		return ErrUnavailable
	}
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(groupID) == "" {
		return ErrValidation
	}
	member, err := s.repos.Groups.GetMember(ctx, groupID, userID)
	if err != nil {
		return mapRepositoryError(err)
	}
	if member.Role == groupRoleOwner {
		if s.tx != nil && s.repos.Files != nil {
			return repository.WithTx(ctx, s.tx, func(repos repository.Repositories) error {
				if err := repos.Groups.SoftDelete(ctx, groupID); err != nil {
					return mapRepositoryError(err)
				}
				_, err := repos.Files.SoftDeleteUsages(ctx, "group_avatar", groupID)
				return mapRepositoryError(err)
			})
		}
		return mapRepositoryError(s.repos.Groups.SoftDelete(ctx, groupID))
	}
	return mapRepositoryError(s.repos.Groups.RemoveMember(ctx, groupID, userID))
}

func (s *Service) AddMembers(ctx context.Context, input AddMembersInput) ([]repository.GroupMember, error) {
	if s.repos.Groups == nil {
		return nil, ErrUnavailable
	}
	if strings.TrimSpace(input.UserID) == "" || strings.TrimSpace(input.GroupID) == "" {
		return nil, ErrValidation
	}
	userIDs := uniqueUserIDs(input.UserIDs)
	if len(userIDs) == 0 || len(userIDs) > maxGroupMemberBatch {
		return nil, ErrValidation
	}
	viewer, err := s.repos.Groups.GetMember(ctx, input.GroupID, input.UserID)
	if err != nil {
		return nil, mapRepositoryError(err)
	}
	if !canManageGroup(viewer.Role) {
		return nil, ErrForbidden
	}

	if err := s.repos.Groups.AddMembers(ctx, input.GroupID, userIDs, groupRoleMember); err != nil {
		return nil, mapRepositoryError(err)
	}
	if s.repos.Chat != nil {
		if conversationID, found, err := s.repos.Chat.FindGroupConversation(ctx, input.GroupID); err != nil {
			return nil, err
		} else if found {
			if err := s.repos.Chat.AddConversationParticipants(ctx, conversationID, userIDs); err != nil {
				return nil, err
			}
		}
	}

	members := make([]repository.GroupMember, 0, len(userIDs))
	for _, userID := range userIDs {
		member, err := s.repos.Groups.GetMember(ctx, input.GroupID, userID)
		if err != nil {
			return nil, mapRepositoryError(err)
		}
		members = append(members, member)
	}
	return members, nil
}

func (s *Service) RemoveMember(ctx context.Context, input RemoveMemberInput) error {
	if s.repos.Groups == nil {
		return ErrUnavailable
	}
	if strings.TrimSpace(input.UserID) == "" || strings.TrimSpace(input.GroupID) == "" || strings.TrimSpace(input.TargetUserID) == "" {
		return ErrValidation
	}
	viewer, err := s.repos.Groups.GetMember(ctx, input.GroupID, input.UserID)
	if err != nil {
		return mapRepositoryError(err)
	}
	target, err := s.repos.Groups.GetMember(ctx, input.GroupID, input.TargetUserID)
	if err != nil {
		return mapRepositoryError(err)
	}
	if input.UserID == input.TargetUserID {
		if viewer.Role == groupRoleOwner {
			return ErrForbidden
		}
		return mapRepositoryError(s.repos.Groups.RemoveMember(ctx, input.GroupID, input.TargetUserID))
	}
	if !canManageGroup(viewer.Role) || target.Role == groupRoleOwner || (viewer.Role == groupRoleAdmin && target.Role == groupRoleAdmin) {
		return ErrForbidden
	}
	return mapRepositoryError(s.repos.Groups.RemoveMember(ctx, input.GroupID, input.TargetUserID))
}

func (s *Service) UpdateMember(ctx context.Context, input UpdateMemberInput) (repository.GroupMember, error) {
	if s.repos.Groups == nil {
		return repository.GroupMember{}, ErrUnavailable
	}
	if strings.TrimSpace(input.UserID) == "" || strings.TrimSpace(input.GroupID) == "" || strings.TrimSpace(input.TargetUserID) == "" {
		return repository.GroupMember{}, ErrValidation
	}
	role := normalizedOptional(input.Role)
	if role != nil && *role != groupRoleAdmin && *role != groupRoleMember {
		return repository.GroupMember{}, ErrValidation
	}
	nickname, err := validateOptionalText(input.Nickname, maxGroupNicknameLength, false)
	if err != nil {
		return repository.GroupMember{}, err
	}
	viewer, err := s.repos.Groups.GetMember(ctx, input.GroupID, input.UserID)
	if err != nil {
		return repository.GroupMember{}, mapRepositoryError(err)
	}
	target, err := s.repos.Groups.GetMember(ctx, input.GroupID, input.TargetUserID)
	if err != nil {
		return repository.GroupMember{}, mapRepositoryError(err)
	}
	if role != nil {
		if viewer.Role != groupRoleOwner || target.Role == groupRoleOwner {
			return repository.GroupMember{}, ErrForbidden
		}
	} else if input.UserID != input.TargetUserID {
		return repository.GroupMember{}, ErrForbidden
	}

	member, err := s.repos.Groups.UpdateMember(ctx, repository.UpdateGroupMemberParams{
		GroupID:     input.GroupID,
		UserID:      input.TargetUserID,
		RoleSet:     role != nil,
		Role:        role,
		NicknameSet: input.NicknameSet,
		Nickname:    nickname,
		IsDND:       input.IsDND,
	})
	if err != nil {
		return repository.GroupMember{}, mapRepositoryError(err)
	}
	return member, nil
}

func (s *Service) TransferOwner(ctx context.Context, input TransferOwnerInput) (repository.GroupListEntry, error) {
	if s.repos.Groups == nil || s.tx == nil {
		return repository.GroupListEntry{}, ErrUnavailable
	}
	if strings.TrimSpace(input.UserID) == "" || strings.TrimSpace(input.GroupID) == "" || strings.TrimSpace(input.TargetUserID) == "" || input.UserID == input.TargetUserID {
		return repository.GroupListEntry{}, ErrValidation
	}

	if err := repository.WithTx(ctx, s.tx, func(repos repository.Repositories) error {
		viewer, err := repos.Groups.GetMember(ctx, input.GroupID, input.UserID)
		if err != nil {
			return mapRepositoryError(err)
		}
		if viewer.Role != groupRoleOwner {
			return ErrForbidden
		}
		if _, err := repos.Groups.GetMember(ctx, input.GroupID, input.TargetUserID); err != nil {
			return mapRepositoryError(err)
		}
		if _, err := repos.Groups.UpdateOwner(ctx, input.GroupID, input.TargetUserID); err != nil {
			return mapRepositoryError(err)
		}
		if _, err := repos.Groups.UpdateMember(ctx, repository.UpdateGroupMemberParams{
			GroupID: input.GroupID,
			UserID:  input.UserID,
			RoleSet: true,
			Role:    stringPtr(groupRoleAdmin),
		}); err != nil {
			return mapRepositoryError(err)
		}
		if _, err := repos.Groups.UpdateMember(ctx, repository.UpdateGroupMemberParams{
			GroupID: input.GroupID,
			UserID:  input.TargetUserID,
			RoleSet: true,
			Role:    stringPtr(groupRoleOwner),
		}); err != nil {
			return mapRepositoryError(err)
		}
		return nil
	}); err != nil {
		return repository.GroupListEntry{}, err
	}

	return s.repos.Groups.GetForUser(ctx, input.UserID, input.GroupID)
}

func (s *Service) UpdateMySettings(ctx context.Context, input UpdateMySettingsInput) (repository.GroupUserSettings, error) {
	if s.repos.Groups == nil {
		return repository.GroupUserSettings{}, ErrUnavailable
	}
	if strings.TrimSpace(input.UserID) == "" || strings.TrimSpace(input.GroupID) == "" {
		return repository.GroupUserSettings{}, ErrValidation
	}
	if _, err := s.repos.Groups.GetMember(ctx, input.GroupID, input.UserID); err != nil {
		return repository.GroupUserSettings{}, mapRepositoryError(err)
	}
	remark, err := validateOptionalText(input.Remark, maxGroupNicknameLength, false)
	if err != nil {
		return repository.GroupUserSettings{}, err
	}
	listGroupName, err := validateOptionalText(input.ListGroupName, maxGroupListNameLength, false)
	if err != nil {
		return repository.GroupUserSettings{}, err
	}
	settings, err := s.repos.Groups.UpsertUserSettings(ctx, repository.UpsertGroupUserSettingsParams{
		GroupID:          input.GroupID,
		UserID:           input.UserID,
		RemarkSet:        input.RemarkSet,
		Remark:           remark,
		ListGroupIDSet:   input.ListGroupIDSet,
		ListGroupID:      normalizedOptional(input.ListGroupID),
		ListGroupNameSet: input.ListGroupNameSet,
		ListGroupName:    listGroupName,
		IsDND:            input.IsDND,
	})
	if err != nil {
		return repository.GroupUserSettings{}, mapRepositoryError(err)
	}
	return settings, nil
}

func (s *Service) CreateJoinRequest(ctx context.Context, input CreateJoinRequestInput) (repository.GroupJoinRequest, error) {
	if s.repos.Groups == nil || s.repos.Users == nil {
		return repository.GroupJoinRequest{}, ErrUnavailable
	}
	if strings.TrimSpace(input.UserID) == "" || strings.TrimSpace(input.GroupID) == "" {
		return repository.GroupJoinRequest{}, ErrValidation
	}
	message, err := validateOptionalText(input.Message, maxGroupJoinMessageLength, false)
	if err != nil {
		return repository.GroupJoinRequest{}, err
	}
	if user, err := s.repos.Users.GetByID(ctx, input.UserID); err != nil {
		return repository.GroupJoinRequest{}, mapRepositoryError(err)
	} else if user.Status != "active" {
		return repository.GroupJoinRequest{}, ErrForbidden
	}
	if _, err := s.repos.Groups.Get(ctx, input.GroupID); err != nil {
		return repository.GroupJoinRequest{}, mapRepositoryError(err)
	}
	if _, err := s.repos.Groups.GetMember(ctx, input.GroupID, input.UserID); err == nil {
		return repository.GroupJoinRequest{}, ErrConflict
	} else if !errors.Is(err, repository.ErrNotFound) {
		return repository.GroupJoinRequest{}, err
	}
	existing, err := s.repos.Groups.GetPendingJoinRequest(ctx, input.GroupID, input.UserID)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, repository.ErrNotFound) {
		return repository.GroupJoinRequest{}, err
	}
	request, err := s.repos.Groups.CreateJoinRequest(ctx, input.GroupID, input.UserID, message)
	if err != nil {
		return repository.GroupJoinRequest{}, mapRepositoryError(err)
	}
	return request, nil
}

func (s *Service) ListJoinRequests(ctx context.Context, input ListJoinRequestsInput) ([]repository.GroupJoinRequest, int, int, error) {
	if s.repos.Groups == nil {
		return nil, 0, 0, ErrUnavailable
	}
	if strings.TrimSpace(input.UserID) == "" || input.Limit <= 0 || input.Offset < 0 {
		return nil, 0, 0, ErrValidation
	}
	requests, total, unread, err := s.repos.Groups.ListJoinRequests(ctx, input.UserID, input.Limit, input.Offset)
	if err != nil {
		return nil, 0, 0, mapRepositoryError(err)
	}
	return requests, total, unread, nil
}

func (s *Service) AcceptJoinRequest(ctx context.Context, userID string, requestID string) (repository.GroupJoinRequest, error) {
	if s.repos.Groups == nil || s.tx == nil {
		return repository.GroupJoinRequest{}, ErrUnavailable
	}
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(requestID) == "" {
		return repository.GroupJoinRequest{}, ErrValidation
	}
	var accepted repository.GroupJoinRequest
	if err := repository.WithTx(ctx, s.tx, func(repos repository.Repositories) error {
		request, err := repos.Groups.AcceptJoinRequest(ctx, requestID, userID)
		if err != nil {
			return mapRepositoryError(err)
		}
		if err := repos.Groups.AddMembers(ctx, request.Group.ID, []string{request.ActorUser.ID}, groupRoleMember); err != nil {
			return mapRepositoryError(err)
		}
		if repos.Chat != nil {
			if conversationID, found, err := repos.Chat.FindGroupConversation(ctx, request.Group.ID); err != nil {
				return err
			} else if found {
				if err := repos.Chat.AddConversationParticipants(ctx, conversationID, []string{request.ActorUser.ID}); err != nil {
					return err
				}
			}
		}
		accepted = request
		return nil
	}); err != nil {
		return repository.GroupJoinRequest{}, err
	}
	return accepted, nil
}

func (s *Service) RejectJoinRequest(ctx context.Context, userID string, requestID string) (repository.GroupJoinRequest, error) {
	if s.repos.Groups == nil {
		return repository.GroupJoinRequest{}, ErrUnavailable
	}
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(requestID) == "" {
		return repository.GroupJoinRequest{}, ErrValidation
	}
	request, err := s.repos.Groups.RejectJoinRequest(ctx, requestID, userID)
	if err != nil {
		return repository.GroupJoinRequest{}, mapRepositoryError(err)
	}
	return request, nil
}

func canManageGroup(role string) bool {
	return role == groupRoleOwner || role == groupRoleAdmin
}

func validateOwnedGroupAvatarFile(ctx context.Context, repos repository.Repositories, userID string, fileID string) error {
	if repos.Files == nil {
		return ErrUnavailable
	}
	file, err := repos.Files.Get(ctx, fileID)
	if err != nil {
		return mapRepositoryError(err)
	}
	if file.OwnerUserID == nil || *file.OwnerUserID != userID {
		return ErrForbidden
	}
	switch file.StorageStatus {
	case "ready", "processing", "uploaded":
		return nil
	default:
		return ErrValidation
	}
}

func uniqueUserIDs(userIDs []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(userIDs))
	for _, userID := range userIDs {
		userID = strings.TrimSpace(userID)
		if userID == "" {
			continue
		}
		if _, ok := seen[userID]; ok {
			continue
		}
		seen[userID] = struct{}{}
		out = append(out, userID)
	}
	return out
}

func withoutUser(userIDs []string, excluded string) []string {
	out := make([]string, 0, len(userIDs))
	for _, userID := range userIDs {
		if userID != excluded {
			out = append(out, userID)
		}
	}
	return out
}

func validateOptionalText(value *string, maxRunes int, requiredWhenSet bool) (*string, error) {
	if value == nil {
		return nil, nil
	}
	normalized := strings.TrimSpace(*value)
	if requiredWhenSet && normalized == "" {
		return nil, ErrValidation
	}
	if len([]rune(normalized)) > maxRunes {
		return nil, ErrValidation
	}
	return &normalized, nil
}

func normalizedOptional(value *string) *string {
	if value == nil {
		return nil
	}
	normalized := strings.TrimSpace(*value)
	if normalized == "" {
		return nil
	}
	return &normalized
}

func stringPtr(value string) *string {
	return &value
}

func eventPayload(key string, value string) json.RawMessage {
	payload, _ := json.Marshal(map[string]string{key: value})
	return payload
}

func mapRepositoryError(err error) error {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return err
	case errors.Is(err, repository.ErrConflict):
		return ErrConflict
	default:
		return err
	}
}

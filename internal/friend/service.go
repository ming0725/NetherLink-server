package friend

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"nlserver/internal/repository"
)

var (
	ErrValidation            = errors.New("friend: validation failed")
	ErrConflict              = errors.New("friend: conflict")
	ErrForbidden             = errors.New("friend: forbidden")
	ErrUnavailable           = errors.New("friend: unavailable")
	ErrRequestAlreadyHandled = errors.New("friend: request already handled")
	ErrRequestNotRecipient   = errors.New("friend: request not recipient")
)

const (
	maxFriendGroupNameLength = 64
	maxRemarkLength          = 64
	maxRequestMessageLength  = 256
	maxRequestSourceLength   = 32
	friendAcceptedMessage    = "我们已经是好友了，现在可以开始聊天了。"
)

type Service struct {
	repos repository.Repositories
	tx    repository.TxBeginner
}

type CreateGroupInput struct {
	UserID    string
	Name      string
	SortOrder int
}

type UpdateGroupInput struct {
	UserID        string
	FriendGroupID string
	Name          *string
	SortOrder     *int
}

type UpdateFriendInput struct {
	UserID       string
	FriendUserID string
	RemarkSet    bool
	Remark       *string
	GroupIDSet   bool
	GroupID      *string
	IsDND        *bool
}

type CreateRequestInput struct {
	FromUserID     string
	ToUserID       string
	Message        *string
	SourceType     *string
	SourceGroupID  *string
	SourceFriendID *string
}

type ListRequestsInput struct {
	UserID string
	Status string
	Limit  int
	Offset int
}

type AcceptRequestInput struct {
	UserID    string
	RequestID string
	Remark    *string
	GroupID   *string
}

type AcceptRequestResult struct {
	Friend         repository.Friend
	ConversationID string
	Message        *repository.Message
}

type RequestActionError struct {
	Err       error
	Action    string
	ActorRole string
	Request   repository.FriendRequest
}

func (e *RequestActionError) Error() string {
	if e == nil {
		return ""
	}
	return e.Err.Error() + ": action=" + e.Action + " status=" + e.Request.Status + " role=" + e.ActorRole
}

func (e *RequestActionError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func NewService(repos repository.Repositories, tx repository.TxBeginner) *Service {
	return &Service{repos: repos, tx: tx}
}

func (s *Service) CreateGroup(ctx context.Context, input CreateGroupInput) (repository.FriendGroup, error) {
	name := strings.TrimSpace(input.Name)
	if strings.TrimSpace(input.UserID) == "" || name == "" || len([]rune(name)) > maxFriendGroupNameLength || input.SortOrder < 0 {
		return repository.FriendGroup{}, ErrValidation
	}

	group, err := s.repos.Friends.CreateGroup(ctx, repository.CreateFriendGroupParams{
		UserID:    input.UserID,
		Name:      name,
		SortOrder: input.SortOrder,
	})
	if err != nil {
		return repository.FriendGroup{}, mapRepositoryError(err)
	}

	return group, nil
}

func (s *Service) UpdateGroup(ctx context.Context, input UpdateGroupInput) (repository.FriendGroup, error) {
	if strings.TrimSpace(input.UserID) == "" || strings.TrimSpace(input.FriendGroupID) == "" {
		return repository.FriendGroup{}, ErrValidation
	}

	params := repository.UpdateFriendGroupParams{
		UserID:        input.UserID,
		FriendGroupID: input.FriendGroupID,
		NameSet:       input.Name != nil,
		SortOrderSet:  input.SortOrder != nil,
	}
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" || len([]rune(name)) > maxFriendGroupNameLength {
			return repository.FriendGroup{}, ErrValidation
		}
		params.Name = &name
	}
	if input.SortOrder != nil {
		if *input.SortOrder < 0 {
			return repository.FriendGroup{}, ErrValidation
		}
		params.SortOrder = *input.SortOrder
	}

	group, err := s.repos.Friends.UpdateGroup(ctx, params)
	if err != nil {
		return repository.FriendGroup{}, mapRepositoryError(err)
	}

	return group, nil
}

func (s *Service) DeleteGroup(ctx context.Context, userID string, friendGroupID string) error {
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(friendGroupID) == "" {
		return ErrValidation
	}
	if err := s.repos.Friends.DeleteGroup(ctx, userID, friendGroupID); err != nil {
		return mapRepositoryError(err)
	}
	return nil
}

func (s *Service) UpdateFriend(ctx context.Context, input UpdateFriendInput) (repository.Friend, error) {
	if strings.TrimSpace(input.UserID) == "" || strings.TrimSpace(input.FriendUserID) == "" {
		return repository.Friend{}, ErrValidation
	}

	params := repository.UpdateFriendParams{
		UserID:       input.UserID,
		FriendUserID: input.FriendUserID,
		RemarkSet:    input.RemarkSet,
		GroupIDSet:   input.GroupIDSet,
		IsDND:        input.IsDND,
	}
	if input.RemarkSet {
		remark := strings.TrimSpace(stringValue(input.Remark))
		if len([]rune(remark)) > maxRemarkLength {
			return repository.Friend{}, ErrValidation
		}
		params.Remark = &remark
	}
	if input.GroupIDSet {
		groupID := strings.TrimSpace(stringValue(input.GroupID))
		if groupID != "" {
			ok, err := s.repos.Friends.GroupExists(ctx, input.UserID, groupID)
			if err != nil {
				return repository.Friend{}, err
			}
			if !ok {
				return repository.Friend{}, ErrValidation
			}
		}
		params.GroupID = &groupID
	}

	updated, err := s.repos.Friends.UpdateFriend(ctx, params)
	if err != nil {
		return repository.Friend{}, mapRepositoryError(err)
	}

	return updated, nil
}

func (s *Service) DeleteFriend(ctx context.Context, userID string, friendUserID string) error {
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(friendUserID) == "" || userID == friendUserID {
		return ErrValidation
	}
	if err := s.repos.Friends.DeleteFriend(ctx, userID, friendUserID); err != nil {
		return mapRepositoryError(err)
	}
	return nil
}

func (s *Service) CreateRequest(ctx context.Context, input CreateRequestInput) (repository.FriendRequest, error) {
	fromUserID := strings.TrimSpace(input.FromUserID)
	toUserID := strings.TrimSpace(input.ToUserID)
	if fromUserID == "" || toUserID == "" || fromUserID == toUserID {
		return repository.FriendRequest{}, ErrValidation
	}

	if err := s.validateRequestOptionalFields(input); err != nil {
		return repository.FriendRequest{}, err
	}

	toUser, err := s.repos.Users.GetByID(ctx, toUserID)
	if err != nil {
		return repository.FriendRequest{}, mapRepositoryError(err)
	}
	if toUser.Status != "active" {
		return repository.FriendRequest{}, ErrForbidden
	}

	exists, err := s.repos.Friends.ActiveFriendshipExists(ctx, fromUserID, toUserID)
	if err != nil {
		return repository.FriendRequest{}, err
	}
	if exists {
		return repository.FriendRequest{}, ErrConflict
	}

	existing, err := s.repos.Friends.GetPendingFriendRequest(ctx, fromUserID, toUserID)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, repository.ErrNotFound) {
		return repository.FriendRequest{}, err
	}

	request, err := s.repos.Friends.CreateFriendRequest(ctx, repository.CreateFriendRequestParams{
		FromUserID:     fromUserID,
		ToUserID:       toUserID,
		Message:        normalizedOptional(input.Message),
		SourceType:     normalizedOptional(input.SourceType),
		SourceGroupID:  normalizedOptional(input.SourceGroupID),
		SourceFriendID: normalizedOptional(input.SourceFriendID),
	})
	if err != nil {
		return repository.FriendRequest{}, mapRepositoryError(err)
	}

	return request, nil
}

func (s *Service) ListRequests(ctx context.Context, input ListRequestsInput) ([]repository.FriendRequest, int, int, error) {
	status := strings.TrimSpace(input.Status)
	if strings.TrimSpace(input.UserID) == "" || !validRequestStatusFilter(status) {
		return nil, 0, 0, ErrValidation
	}

	requests, total, unread, err := s.repos.Friends.ListFriendRequests(ctx, input.UserID, status, input.Limit, input.Offset)
	if err != nil {
		return nil, 0, 0, mapRepositoryError(err)
	}

	return requests, total, unread, nil
}

func (s *Service) AcceptRequest(ctx context.Context, input AcceptRequestInput) (AcceptRequestResult, error) {
	if strings.TrimSpace(input.UserID) == "" || strings.TrimSpace(input.RequestID) == "" {
		return AcceptRequestResult{}, ErrValidation
	}

	remark := strings.TrimSpace(stringValue(input.Remark))
	if len([]rune(remark)) > maxRemarkLength {
		return AcceptRequestResult{}, ErrValidation
	}
	groupID := strings.TrimSpace(stringValue(input.GroupID))
	if groupID != "" {
		ok, err := s.repos.Friends.GroupExists(ctx, input.UserID, groupID)
		if err != nil {
			return AcceptRequestResult{}, err
		}
		if !ok {
			return AcceptRequestResult{}, ErrValidation
		}
	}

	var result AcceptRequestResult
	if err := s.withTx(ctx, func(repos repository.Repositories) error {
		request, err := repos.Friends.AcceptFriendRequest(ctx, input.RequestID, input.UserID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				stateErr := friendRequestActionError(ctx, repos, input.UserID, input.RequestID, "accept")
				var actionErr *RequestActionError
				if errors.As(stateErr, &actionErr) && errors.Is(actionErr, ErrRequestAlreadyHandled) && actionErr.Request.Status == "accepted" {
					friend, relationErr := repos.Friends.GetRelation(ctx, input.UserID, actionErr.Request.FromUser.ID)
					if relationErr == nil {
						result.Friend = friend
						return nil
					}
				}
				return stateErr
			}
			return mapRepositoryError(err)
		}

		currentFriend, err := repos.Friends.UpsertFriendship(ctx, repository.UpsertFriendshipParams{
			UserID:        input.UserID,
			FriendUserID:  request.FromUser.ID,
			Remark:        nilIfEmpty(remark),
			FriendGroupID: nilIfEmpty(groupID),
			IsDND:         false,
		})
		if err != nil {
			return mapRepositoryError(err)
		}

		if _, err := repos.Friends.UpsertFriendship(ctx, repository.UpsertFriendshipParams{
			UserID:       request.FromUser.ID,
			FriendUserID: input.UserID,
			IsDND:        false,
		}); err != nil {
			return mapRepositoryError(err)
		}

		result.Friend = currentFriend
		conversationID, message, err := createFriendAcceptedMessage(ctx, repos, input.UserID, request.FromUser.ID, request.ID)
		if err != nil {
			return mapRepositoryError(err)
		}
		result.ConversationID = conversationID
		result.Message = &message
		return nil
	}); err != nil {
		return AcceptRequestResult{}, err
	}

	return result, nil
}

func createFriendAcceptedMessage(ctx context.Context, repos repository.Repositories, senderID string, recipientID string, requestID string) (string, repository.Message, error) {
	conversationID, found, err := repos.Chat.FindDirectConversation(ctx, senderID, recipientID)
	if err != nil {
		return "", repository.Message{}, err
	}
	if !found {
		peerUserID := recipientID
		conversation, err := repos.Chat.CreateConversation(ctx, repository.CreateConversationParams{
			Type:       "direct",
			PeerUserID: &peerUserID,
		})
		if err != nil {
			return "", repository.Message{}, err
		}
		conversationID = conversation.ID
	}
	if err := repos.Chat.AddConversationParticipants(ctx, conversationID, []string{senderID, recipientID}); err != nil {
		return "", repository.Message{}, err
	}

	seq, err := repos.Chat.NextMessageSeq(ctx, conversationID)
	if err != nil {
		return "", repository.Message{}, err
	}
	contentText := friendAcceptedMessage
	message, err := repos.Chat.CreateMessage(ctx, repository.CreateMessageParams{
		ConversationID:  conversationID,
		SenderID:        senderID,
		MessageSeq:      seq,
		Type:            "text",
		ContentText:     &contentText,
		ContentJSON:     json.RawMessage(`{}`),
		ClientMessageID: "friend-request-accepted:" + requestID,
	})
	if err != nil {
		return "", repository.Message{}, err
	}
	if err := repos.Chat.TouchConversationAfterMessage(ctx, conversationID, message.ID, senderID); err != nil {
		return "", repository.Message{}, err
	}
	return conversationID, message, nil
}

func (s *Service) RejectRequest(ctx context.Context, userID string, requestID string) (repository.FriendRequest, error) {
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(requestID) == "" {
		return repository.FriendRequest{}, ErrValidation
	}

	request, err := s.repos.Friends.RejectFriendRequest(ctx, requestID, userID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			stateErr := friendRequestActionError(ctx, s.repos, userID, requestID, "reject")
			var actionErr *RequestActionError
			if errors.As(stateErr, &actionErr) && errors.Is(actionErr, ErrRequestAlreadyHandled) && actionErr.Request.Status == "rejected" {
				return actionErr.Request, nil
			}
			return repository.FriendRequest{}, stateErr
		}
		return repository.FriendRequest{}, mapRepositoryError(err)
	}

	return request, nil
}

func friendRequestActionError(ctx context.Context, repos repository.Repositories, userID string, requestID string, action string) error {
	request, err := repos.Friends.GetFriendRequest(ctx, requestID)
	if err != nil {
		return mapRepositoryError(err)
	}

	role := ""
	switch userID {
	case request.ToUser.ID:
		role = "recipient"
	case request.FromUser.ID:
		role = "sender"
	default:
		return repository.ErrNotFound
	}

	if role != "recipient" {
		return &RequestActionError{
			Err:       ErrRequestNotRecipient,
			Action:    action,
			ActorRole: role,
			Request:   request,
		}
	}
	if request.Status != "pending" {
		return &RequestActionError{
			Err:       ErrRequestAlreadyHandled,
			Action:    action,
			ActorRole: role,
			Request:   request,
		}
	}

	return repository.ErrNotFound
}

func (s *Service) validateRequestOptionalFields(input CreateRequestInput) error {
	message := strings.TrimSpace(stringValue(input.Message))
	if len([]rune(message)) > maxRequestMessageLength {
		return ErrValidation
	}
	sourceType := strings.TrimSpace(stringValue(input.SourceType))
	if len([]rune(sourceType)) > maxRequestSourceLength {
		return ErrValidation
	}
	return nil
}

func (s *Service) withTx(ctx context.Context, fn func(repository.Repositories) error) error {
	if s.tx == nil {
		return ErrUnavailable
	}
	return repository.WithTx(ctx, s.tx, fn)
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

func validRequestStatusFilter(status string) bool {
	switch status {
	case "", "pending", "accepted", "rejected":
		return true
	default:
		return false
	}
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
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

func nilIfEmpty(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

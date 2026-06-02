package post

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"nlserver/internal/repository"
)

var (
	ErrValidation  = errors.New("post: validation failed")
	ErrForbidden   = errors.New("post: forbidden")
	ErrConflict    = errors.New("post: conflict")
	ErrUnavailable = errors.New("post: unavailable")
)

const (
	maxTitleLength   = 120
	maxContentLength = 10000
	maxMediaItems    = 20
	maxCommentLength = 2000
)

type Service struct {
	repos repository.Repositories
	tx    repository.TxBeginner
}

type MediaInput struct {
	FileID      string
	MediaType   string
	SortOrder   int
	Width       *int
	Height      *int
	DurationMS  *int
	CoverFileID *string
	Playback    json.RawMessage
	LiveStatus  *string
	Metadata    json.RawMessage
}

type CreatePostInput struct {
	AuthorID         string
	Title            *string
	Content          string
	ContentCreatedAt *time.Time
	Visibility       string
	Media            []MediaInput
}

type UpdatePostInput struct {
	PostID     string
	AuthorID   string
	TitleSet   bool
	Title      *string
	ContentSet bool
	Content    string
	Visibility *string
	MediaSet   bool
	Media      []MediaInput
}

type SetLikeInput struct {
	UserID string
	PostID string
	Liked  bool
}

type SetFollowInput struct {
	FollowerUserID string
	TargetUserID   string
	Followed       bool
}

type CreateCommentInput struct {
	PostID   string
	AuthorID string
	Content  string
}

type CreateReplyInput struct {
	CommentID     string
	AuthorID      string
	TargetUserID  *string
	TargetReplyID *string
	Content       string
}

func NewService(repos repository.Repositories, tx repository.TxBeginner) *Service {
	return &Service{repos: repos, tx: tx}
}

func (s *Service) CreatePost(ctx context.Context, input CreatePostInput) (repository.PostEntry, error) {
	if s.repos.Posts == nil || s.tx == nil {
		return repository.PostEntry{}, ErrUnavailable
	}
	title, content, visibility, err := normalizePostFields(input.Title, input.Content, input.Visibility)
	if err != nil {
		return repository.PostEntry{}, err
	}
	if err := s.validateMedia(ctx, input.AuthorID, input.Media); err != nil {
		return repository.PostEntry{}, err
	}

	var postID string
	if err := repository.WithTx(ctx, s.tx, func(repos repository.Repositories) error {
		created, err := repos.Posts.CreatePost(ctx, repository.CreatePostParams{
			AuthorID:         input.AuthorID,
			Title:            title,
			Content:          content,
			ContentCreatedAt: input.ContentCreatedAt,
			Visibility:       visibility,
		})
		if err != nil {
			return mapRepositoryError(err)
		}
		if _, err := repos.Posts.ReplacePostMedia(ctx, created.ID, input.AuthorID, visibility, makeCreateMediaParams(input.Media)); err != nil {
			return mapRepositoryError(err)
		}
		postID = created.ID
		return nil
	}); err != nil {
		return repository.PostEntry{}, err
	}

	return s.repos.Posts.GetPost(ctx, input.AuthorID, postID)
}

func (s *Service) UpdatePost(ctx context.Context, input UpdatePostInput) (repository.PostEntry, error) {
	if s.repos.Posts == nil || s.tx == nil {
		return repository.PostEntry{}, ErrUnavailable
	}
	if strings.TrimSpace(input.PostID) == "" || strings.TrimSpace(input.AuthorID) == "" {
		return repository.PostEntry{}, ErrValidation
	}

	current, err := s.repos.Posts.GetPost(ctx, input.AuthorID, input.PostID)
	if err != nil {
		return repository.PostEntry{}, mapRepositoryError(err)
	}
	if current.Post.AuthorID != input.AuthorID {
		return repository.PostEntry{}, ErrForbidden
	}

	title := current.Post.Title
	if input.TitleSet {
		title = input.Title
	}
	content := current.Post.Content
	if input.ContentSet {
		content = input.Content
	}
	visibility := current.Post.Visibility
	if input.Visibility != nil {
		visibility = *input.Visibility
	}
	title, content, visibility, err = normalizePostFields(title, content, visibility)
	if err != nil {
		return repository.PostEntry{}, err
	}
	if input.MediaSet {
		if err := s.validateMedia(ctx, input.AuthorID, input.Media); err != nil {
			return repository.PostEntry{}, err
		}
	}

	if err := repository.WithTx(ctx, s.tx, func(repos repository.Repositories) error {
		if _, err := repos.Posts.UpdatePost(ctx, repository.UpdatePostParams{
			PostID:     input.PostID,
			AuthorID:   input.AuthorID,
			Title:      title,
			Content:    content,
			Visibility: visibility,
		}); err != nil {
			return mapRepositoryError(err)
		}
		if input.MediaSet {
			if _, err := repos.Posts.ReplacePostMedia(ctx, input.PostID, input.AuthorID, visibility, makeCreateMediaParams(input.Media)); err != nil {
				return mapRepositoryError(err)
			}
		} else if input.Visibility != nil {
			if err := repos.Posts.UpdatePostMediaUsageVisibility(ctx, input.PostID, visibility); err != nil {
				return mapRepositoryError(err)
			}
		}
		return nil
	}); err != nil {
		return repository.PostEntry{}, err
	}

	return s.repos.Posts.GetPost(ctx, input.AuthorID, input.PostID)
}

func (s *Service) DeletePost(ctx context.Context, postID string, actorUserID string) error {
	if strings.TrimSpace(postID) == "" || strings.TrimSpace(actorUserID) == "" {
		return ErrValidation
	}
	if err := s.repos.Posts.DeletePost(ctx, postID, actorUserID); err != nil {
		return mapRepositoryError(err)
	}
	return nil
}

func (s *Service) SetPostLike(ctx context.Context, input SetLikeInput) (int, bool, error) {
	if strings.TrimSpace(input.UserID) == "" || strings.TrimSpace(input.PostID) == "" {
		return 0, false, ErrValidation
	}
	count, liked, err := s.repos.Posts.SetPostLike(ctx, input.PostID, input.UserID, input.Liked)
	return count, liked, mapRepositoryError(err)
}

func (s *Service) SetFollow(ctx context.Context, input SetFollowInput) (bool, error) {
	if strings.TrimSpace(input.FollowerUserID) == "" || strings.TrimSpace(input.TargetUserID) == "" || input.FollowerUserID == input.TargetUserID {
		return false, ErrValidation
	}
	user, err := s.repos.Users.GetByID(ctx, input.TargetUserID)
	if err != nil {
		return false, mapRepositoryError(err)
	}
	if user.Status != "active" {
		return false, ErrForbidden
	}
	followed, err := s.repos.Posts.SetFollow(ctx, input.FollowerUserID, input.TargetUserID, input.Followed)
	return followed, mapRepositoryError(err)
}

func (s *Service) CreateComment(ctx context.Context, input CreateCommentInput) (repository.PostComment, error) {
	content, err := normalizeCommentContent(input.Content)
	if err != nil {
		return repository.PostComment{}, err
	}
	comment, err := s.repos.Posts.CreateComment(ctx, input.PostID, input.AuthorID, content)
	return comment, mapRepositoryError(err)
}

func (s *Service) SetCommentLike(ctx context.Context, commentID string, userID string, liked bool) (int, bool, error) {
	if strings.TrimSpace(commentID) == "" || strings.TrimSpace(userID) == "" {
		return 0, false, ErrValidation
	}
	count, isLiked, err := s.repos.Posts.SetCommentLike(ctx, commentID, userID, liked)
	return count, isLiked, mapRepositoryError(err)
}

func (s *Service) CreateReply(ctx context.Context, input CreateReplyInput) (repository.PostReply, error) {
	content, err := normalizeCommentContent(input.Content)
	if err != nil {
		return repository.PostReply{}, err
	}
	reply, err := s.repos.Posts.CreateReply(ctx, input.CommentID, input.AuthorID, input.TargetUserID, input.TargetReplyID, content)
	return reply, mapRepositoryError(err)
}

func (s *Service) SetReplyLike(ctx context.Context, replyID string, userID string, liked bool) (int, bool, error) {
	if strings.TrimSpace(replyID) == "" || strings.TrimSpace(userID) == "" {
		return 0, false, ErrValidation
	}
	count, isLiked, err := s.repos.Posts.SetReplyLike(ctx, replyID, userID, liked)
	return count, isLiked, mapRepositoryError(err)
}

func (s *Service) DeleteComment(ctx context.Context, commentID string, actorUserID string) error {
	if strings.TrimSpace(commentID) == "" || strings.TrimSpace(actorUserID) == "" {
		return ErrValidation
	}
	return mapRepositoryError(s.repos.Posts.DeleteComment(ctx, commentID, actorUserID))
}

func (s *Service) DeleteReply(ctx context.Context, replyID string, actorUserID string) error {
	if strings.TrimSpace(replyID) == "" || strings.TrimSpace(actorUserID) == "" {
		return ErrValidation
	}
	return mapRepositoryError(s.repos.Posts.DeleteReply(ctx, replyID, actorUserID))
}

func (s *Service) validateMedia(ctx context.Context, ownerUserID string, media []MediaInput) error {
	if strings.TrimSpace(ownerUserID) == "" || len(media) > maxMediaItems {
		return ErrValidation
	}
	for i, item := range media {
		if strings.TrimSpace(item.FileID) == "" || !validMediaType(item.MediaType) || item.SortOrder < 0 {
			return ErrValidation
		}
		if item.Width != nil && *item.Width <= 0 {
			return ErrValidation
		}
		if item.Height != nil && *item.Height <= 0 {
			return ErrValidation
		}
		if item.DurationMS != nil && *item.DurationMS < 0 {
			return ErrValidation
		}
		if len(item.Playback) > 0 && !json.Valid(item.Playback) {
			return ErrValidation
		}
		if len(item.Metadata) > 0 && !json.Valid(item.Metadata) {
			return ErrValidation
		}
		file, err := s.repos.Files.Get(ctx, item.FileID)
		if err != nil {
			return mapRepositoryError(err)
		}
		if file.OwnerUserID != nil && *file.OwnerUserID != ownerUserID {
			return ErrForbidden
		}
		if file.StorageStatus != "ready" && file.StorageStatus != "processing" && file.StorageStatus != "uploaded" {
			return ErrValidation
		}
		if item.CoverFileID != nil && strings.TrimSpace(*item.CoverFileID) != "" {
			coverFile, err := s.repos.Files.Get(ctx, *item.CoverFileID)
			if err != nil {
				return mapRepositoryError(err)
			}
			if coverFile.OwnerUserID != nil && *coverFile.OwnerUserID != ownerUserID {
				return ErrForbidden
			}
		}
		if media[i].SortOrder == 0 {
			media[i].SortOrder = i
		}
	}
	return nil
}

func normalizePostFields(title *string, content string, visibility string) (*string, string, string, error) {
	normalizedContent := strings.TrimSpace(content)
	if normalizedContent == "" || len([]rune(normalizedContent)) > maxContentLength {
		return nil, "", "", ErrValidation
	}
	var normalizedTitle *string
	if title != nil {
		trimmed := strings.TrimSpace(*title)
		if len([]rune(trimmed)) > maxTitleLength {
			return nil, "", "", ErrValidation
		}
		if trimmed != "" {
			normalizedTitle = &trimmed
		}
	}
	normalizedVisibility := strings.TrimSpace(visibility)
	if normalizedVisibility == "" {
		normalizedVisibility = "public"
	}
	if normalizedVisibility != "public" && normalizedVisibility != "private" {
		return nil, "", "", ErrValidation
	}
	return normalizedTitle, normalizedContent, normalizedVisibility, nil
}

func normalizeCommentContent(content string) (string, error) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" || len([]rune(trimmed)) > maxCommentLength {
		return "", ErrValidation
	}
	return trimmed, nil
}

func validMediaType(mediaType string) bool {
	switch strings.TrimSpace(mediaType) {
	case "image", "video", "live", "audio", "file":
		return true
	default:
		return false
	}
}

func makeCreateMediaParams(media []MediaInput) []repository.CreatePostMediaParams {
	params := make([]repository.CreatePostMediaParams, 0, len(media))
	for i, item := range media {
		sortOrder := item.SortOrder
		if sortOrder == 0 {
			sortOrder = i
		}
		params = append(params, repository.CreatePostMediaParams{
			FileID:      item.FileID,
			MediaType:   strings.TrimSpace(item.MediaType),
			SortOrder:   sortOrder,
			Width:       item.Width,
			Height:      item.Height,
			DurationMS:  item.DurationMS,
			CoverFileID: item.CoverFileID,
			Playback:    item.Playback,
			LiveStatus:  item.LiveStatus,
			Metadata:    item.Metadata,
		})
	}
	return params
}

func mapRepositoryError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, repository.ErrNotFound):
		return repository.ErrNotFound
	case errors.Is(err, repository.ErrConflict):
		return ErrConflict
	default:
		return err
	}
}

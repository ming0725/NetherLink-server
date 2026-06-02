package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	postsvc "nlserver/internal/post"
	"nlserver/internal/repository"
)

type postMediaRequest struct {
	FileID      string          `json:"fileId"`
	MediaType   string          `json:"mediaType"`
	SortOrder   int             `json:"sortOrder"`
	Width       *int            `json:"width"`
	Height      *int            `json:"height"`
	DurationMS  *int            `json:"durationMs"`
	CoverFileID *string         `json:"coverFileId"`
	Playback    json.RawMessage `json:"playback"`
	LiveStatus  *string         `json:"liveStatus"`
	Metadata    json.RawMessage `json:"metadata"`
}

type createPostRequest struct {
	Title            *string            `json:"title"`
	Content          string             `json:"content"`
	ContentCreatedAt *time.Time         `json:"contentCreatedAt"`
	Visibility       string             `json:"visibility"`
	Media            []postMediaRequest `json:"media"`
}

type patchPostRequest struct {
	Title           *string             `json:"title"`
	Content         *string             `json:"content"`
	Visibility      *string             `json:"visibility"`
	Media           *[]postMediaRequest `json:"media"`
	ExpectedVersion *int                `json:"expectedVersion"`
}

type setLikedRequest struct {
	Liked bool `json:"liked"`
}

type setFollowedRequest struct {
	Followed bool `json:"followed"`
}

type createPostCommentRequest struct {
	Content string `json:"content"`
}

type createPostReplyRequest struct {
	Content        string  `json:"content"`
	TargetUserUUID *string `json:"targetUserUuid"`
	TargetReplyID  *string `json:"targetReplyId"`
}

type postsResponse struct {
	Posts []postResponse `json:"posts"`
	Total int            `json:"total"`
}

type postDetailResponse struct {
	Post postResponse `json:"post"`
}

type postResponse struct {
	PostID           string              `json:"postId"`
	Author           userResponse        `json:"author"`
	Title            *string             `json:"title,omitempty"`
	Content          string              `json:"content"`
	CoverMediaID     *string             `json:"coverMediaId,omitempty"`
	MediaPreview     []postMediaResponse `json:"mediaPreview"`
	Media            []postMediaResponse `json:"media,omitempty"`
	LikeCount        int                 `json:"likeCount"`
	CommentCount     int                 `json:"commentCount"`
	ContentCreatedAt *time.Time          `json:"contentCreatedAt,omitempty"`
	Visibility       string              `json:"visibility"`
	Viewer           postViewerResponse  `json:"viewer"`
	Version          int                 `json:"version"`
	ETag             string              `json:"etag"`
	CreatedAt        time.Time           `json:"createdAt"`
	UpdatedAt        time.Time           `json:"updatedAt"`
}

type postMediaResponse struct {
	MediaID     string          `json:"mediaId"`
	FileID      string          `json:"fileId"`
	MediaType   string          `json:"mediaType"`
	SortOrder   int             `json:"sortOrder"`
	Width       *int            `json:"width,omitempty"`
	Height      *int            `json:"height,omitempty"`
	DurationMS  *int            `json:"durationMs,omitempty"`
	CoverFileID *string         `json:"coverFileId,omitempty"`
	Playback    json.RawMessage `json:"playback"`
	LiveStatus  *string         `json:"liveStatus,omitempty"`
	Metadata    json.RawMessage `json:"metadata"`
}

type postViewerResponse struct {
	IsLiked          bool `json:"isLiked"`
	IsFollowedAuthor bool `json:"isFollowedAuthor"`
}

type postLikeResponse struct {
	LikeCount int  `json:"likeCount"`
	IsLiked   bool `json:"isLiked"`
}

type followResponse struct {
	IsFollowedAuthor bool `json:"isFollowedAuthor"`
}

type postCommentsResponse struct {
	Comments []postCommentResponse `json:"comments"`
	Total    int                   `json:"total"`
	HasMore  bool                  `json:"hasMore"`
}

type postCommentDetailResponse struct {
	Comment postCommentResponse `json:"comment"`
}

type postReplyDetailResponse struct {
	Reply postReplyResponse `json:"reply"`
}

type commentLikeResponse struct {
	LikeCount int  `json:"likeCount"`
	IsLiked   bool `json:"isLiked"`
}

type postCommentResponse struct {
	CommentID  string              `json:"commentId"`
	PostID     string              `json:"postId"`
	Author     userResponse        `json:"author"`
	Content    string              `json:"content"`
	LikeCount  int                 `json:"likeCount"`
	ReplyCount int                 `json:"replyCount"`
	Viewer     commentViewerState  `json:"viewer"`
	Replies    []postReplyResponse `json:"replies"`
	CreatedAt  time.Time           `json:"createdAt"`
	UpdatedAt  time.Time           `json:"updatedAt"`
}

type postReplyResponse struct {
	ReplyID       string             `json:"replyId"`
	CommentID     string             `json:"commentId"`
	PostID        string             `json:"postId"`
	Author        userResponse       `json:"author"`
	TargetUser    *userResponse      `json:"targetUser,omitempty"`
	TargetReplyID *string            `json:"targetReplyId,omitempty"`
	Content       string             `json:"content"`
	LikeCount     int                `json:"likeCount"`
	Viewer        commentViewerState `json:"viewer"`
	CreatedAt     time.Time          `json:"createdAt"`
	UpdatedAt     time.Time          `json:"updatedAt"`
}

type commentViewerState struct {
	IsLiked bool `json:"isLiked"`
}

func (s *Server) handleListPosts(w http.ResponseWriter, r *http.Request) {
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
	followOnly, reqErr := parseOptionalBool(r.URL.Query().Get("followOnly"), false, "followOnly")
	if reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}

	posts, total, err := s.repos.Posts.ListPosts(r.Context(), principal.UserID, followOnly, limit, offset)
	if err != nil {
		writeRepositoryError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, postsResponse{Posts: makePostResponses(posts, false), Total: total})
}

func (s *Server) handleCreatePost(w http.ResponseWriter, r *http.Request) {
	if !s.requirePostService(w, r) {
		return
	}
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	var req createPostRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeRequestError(w, r, err)
		return
	}
	if reqErr := validatePostMediaRequests(req.Media); reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}
	post, err := s.postService.CreatePost(r.Context(), postsvc.CreatePostInput{
		AuthorID:         principal.UserID,
		Title:            req.Title,
		Content:          req.Content,
		ContentCreatedAt: req.ContentCreatedAt,
		Visibility:       req.Visibility,
		Media:            makePostMediaInputs(req.Media),
	})
	if err != nil {
		writePostServiceError(w, r, err)
		return
	}
	response := postDetailResponse{Post: makePostResponse(post, true)}
	s.publishRealtimeEvent(r.Context(), []string{principal.UserID}, "post.created", response)
	writeJSONWithETag(w, r, http.StatusCreated, response.Post.ETag, response)
}

func (s *Server) handleGetPost(w http.ResponseWriter, r *http.Request) {
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	postID := r.PathValue("postId")
	if reqErr := validateUUID(postID, "postId"); reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}
	post, err := s.repos.Posts.GetPost(r.Context(), principal.UserID, postID)
	if err != nil {
		writeRepositoryError(w, r, err)
		return
	}
	response := postDetailResponse{Post: makePostResponse(post, true)}
	writeJSONWithETag(w, r, http.StatusOK, response.Post.ETag, response)
}

func (s *Server) handlePatchPost(w http.ResponseWriter, r *http.Request) {
	if !s.requirePostService(w, r) {
		return
	}
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	postID := r.PathValue("postId")
	if reqErr := validateUUID(postID, "postId"); reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}
	var req patchPostRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeRequestError(w, r, err)
		return
	}
	media := []postMediaRequest(nil)
	if req.Media != nil {
		media = *req.Media
		if reqErr := validatePostMediaRequests(media); reqErr != nil {
			writeRequestError(w, r, reqErr)
			return
		}
	}
	current, err := s.repos.Posts.GetPost(r.Context(), principal.UserID, postID)
	if err != nil {
		writeRepositoryError(w, r, err)
		return
	}
	if !checkResourceVersion(w, r, resourceETag("post", current.Post.ID, current.Post.Version), current.Post.Version, req.ExpectedVersion) {
		return
	}
	content := ""
	if req.Content != nil {
		content = *req.Content
	}
	post, err := s.postService.UpdatePost(r.Context(), postsvc.UpdatePostInput{
		PostID:     postID,
		AuthorID:   principal.UserID,
		TitleSet:   req.Title != nil,
		Title:      req.Title,
		ContentSet: req.Content != nil,
		Content:    content,
		Visibility: req.Visibility,
		MediaSet:   req.Media != nil,
		Media:      makePostMediaInputs(media),
	})
	if err != nil {
		writePostServiceError(w, r, err)
		return
	}
	response := postDetailResponse{Post: makePostResponse(post, true)}
	s.publishRealtimeEvent(r.Context(), []string{principal.UserID, post.Post.AuthorID}, "post.updated", response)
	writeJSONWithETag(w, r, http.StatusOK, response.Post.ETag, response)
}

func (s *Server) handleDeletePost(w http.ResponseWriter, r *http.Request) {
	if !s.requirePostService(w, r) {
		return
	}
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	postID := r.PathValue("postId")
	if reqErr := validateUUID(postID, "postId"); reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}
	expectedVersion, reqErr := expectedVersionFromQuery(r)
	if reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}
	post, err := s.repos.Posts.GetPost(r.Context(), principal.UserID, postID)
	if err != nil {
		writeRepositoryError(w, r, err)
		return
	}
	if !checkResourceVersion(w, r, resourceETag("post", post.Post.ID, post.Post.Version), post.Post.Version, expectedVersion) {
		return
	}
	if err := s.postService.DeletePost(r.Context(), postID, principal.UserID); err != nil {
		writePostServiceError(w, r, err)
		return
	}
	s.publishRealtimeEvent(r.Context(), []string{principal.UserID, post.Post.AuthorID}, "post.deleted", map[string]any{
		"postId":       postID,
		"authorUuid":   post.Post.AuthorID,
		"operatorUuid": principal.UserID,
	})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleSetPostLike(w http.ResponseWriter, r *http.Request) {
	if !s.requirePostService(w, r) {
		return
	}
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	postID := r.PathValue("postId")
	if reqErr := validateUUID(postID, "postId"); reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}
	var req setLikedRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeRequestError(w, r, err)
		return
	}
	count, liked, err := s.postService.SetPostLike(r.Context(), postsvc.SetLikeInput{UserID: principal.UserID, PostID: postID, Liked: req.Liked})
	if err != nil {
		writePostServiceError(w, r, err)
		return
	}
	response := postLikeResponse{LikeCount: count, IsLiked: liked}
	s.publishPostSnapshotRealtimeEvent(r.Context(), principal.UserID, postID, "post.like.updated", "like.updated", map[string]any{
		"like": response,
	})
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleSetAuthorFollow(w http.ResponseWriter, r *http.Request) {
	if !s.requirePostService(w, r) {
		return
	}
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	authorID := r.PathValue("authorUuid")
	if reqErr := validateUUID(authorID, "authorUuid"); reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}
	var req setFollowedRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeRequestError(w, r, err)
		return
	}
	followed, err := s.postService.SetFollow(r.Context(), postsvc.SetFollowInput{
		FollowerUserID: principal.UserID,
		TargetUserID:   authorID,
		Followed:       req.Followed,
	})
	if err != nil {
		writePostServiceError(w, r, err)
		return
	}
	response := followResponse{IsFollowedAuthor: followed}
	s.publishRealtimeEvent(r.Context(), []string{principal.UserID, authorID}, "post.author.follow.updated", map[string]any{
		"authorUuid":   authorID,
		"followerUuid": principal.UserID,
		"follow":       response,
	})
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleListPostComments(w http.ResponseWriter, r *http.Request) {
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	postID := r.PathValue("postId")
	if reqErr := validateUUID(postID, "postId"); reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}
	limit, offset, reqErr := parsePagination(r)
	if reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}
	if _, err := s.repos.Posts.GetPost(r.Context(), principal.UserID, postID); err != nil {
		writeRepositoryError(w, r, err)
		return
	}
	comments, total, err := s.repos.Posts.ListComments(r.Context(), principal.UserID, postID, limit, offset)
	if err != nil {
		writeRepositoryError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, postCommentsResponse{
		Comments: makePostCommentResponses(comments),
		Total:    total,
		HasMore:  offset+len(comments) < total,
	})
}

func (s *Server) handleCreatePostComment(w http.ResponseWriter, r *http.Request) {
	if !s.requirePostService(w, r) {
		return
	}
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	postID := r.PathValue("postId")
	if reqErr := validateUUID(postID, "postId"); reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}
	var req createPostCommentRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeRequestError(w, r, err)
		return
	}
	comment, err := s.postService.CreateComment(r.Context(), postsvc.CreateCommentInput{
		PostID:   postID,
		AuthorID: principal.UserID,
		Content:  req.Content,
	})
	if err != nil {
		writePostServiceError(w, r, err)
		return
	}
	response := postCommentDetailResponse{Comment: makePostCommentResponse(comment)}
	s.publishPostSnapshotRealtimeEvent(r.Context(), principal.UserID, postID, "post.comment.created", "comment.created", map[string]any{
		"comment": response.Comment,
	})
	writeJSON(w, http.StatusCreated, response)
}

func (s *Server) handleSetCommentLike(w http.ResponseWriter, r *http.Request) {
	if !s.requirePostService(w, r) {
		return
	}
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	commentID := r.PathValue("commentId")
	if reqErr := validateUUID(commentID, "commentId"); reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}
	var req setLikedRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeRequestError(w, r, err)
		return
	}
	count, liked, err := s.postService.SetCommentLike(r.Context(), commentID, principal.UserID, req.Liked)
	if err != nil {
		writePostServiceError(w, r, err)
		return
	}
	response := commentLikeResponse{LikeCount: count, IsLiked: liked}
	s.publishRealtimeEvent(r.Context(), []string{principal.UserID}, "post.comment.like.updated", map[string]any{
		"commentId": commentID,
		"like":      response,
	})
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleCreateCommentReply(w http.ResponseWriter, r *http.Request) {
	if !s.requirePostService(w, r) {
		return
	}
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	commentID := r.PathValue("commentId")
	if reqErr := validateUUID(commentID, "commentId"); reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}
	var req createPostReplyRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeRequestError(w, r, err)
		return
	}
	if req.TargetUserUUID != nil {
		if reqErr := validateOptionalUUID(*req.TargetUserUUID, "targetUserUuid"); reqErr != nil {
			writeRequestError(w, r, reqErr)
			return
		}
	}
	if req.TargetReplyID != nil {
		if reqErr := validateOptionalUUID(*req.TargetReplyID, "targetReplyId"); reqErr != nil {
			writeRequestError(w, r, reqErr)
			return
		}
	}
	reply, err := s.postService.CreateReply(r.Context(), postsvc.CreateReplyInput{
		CommentID:     commentID,
		AuthorID:      principal.UserID,
		TargetUserID:  req.TargetUserUUID,
		TargetReplyID: req.TargetReplyID,
		Content:       req.Content,
	})
	if err != nil {
		writePostServiceError(w, r, err)
		return
	}
	response := postReplyDetailResponse{Reply: makePostReplyResponse(reply)}
	s.publishPostSnapshotRealtimeEvent(r.Context(), principal.UserID, reply.PostID, "post.reply.created", "reply.created", map[string]any{
		"reply": response.Reply,
	})
	writeJSON(w, http.StatusCreated, response)
}

func (s *Server) handleSetReplyLike(w http.ResponseWriter, r *http.Request) {
	if !s.requirePostService(w, r) {
		return
	}
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	replyID := r.PathValue("replyId")
	if reqErr := validateUUID(replyID, "replyId"); reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}
	var req setLikedRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeRequestError(w, r, err)
		return
	}
	count, liked, err := s.postService.SetReplyLike(r.Context(), replyID, principal.UserID, req.Liked)
	if err != nil {
		writePostServiceError(w, r, err)
		return
	}
	response := commentLikeResponse{LikeCount: count, IsLiked: liked}
	s.publishRealtimeEvent(r.Context(), []string{principal.UserID}, "post.reply.like.updated", map[string]any{
		"replyId": replyID,
		"like":    response,
	})
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleDeleteComment(w http.ResponseWriter, r *http.Request) {
	if !s.requirePostService(w, r) {
		return
	}
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	commentID := r.PathValue("commentId")
	if reqErr := validateUUID(commentID, "commentId"); reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}
	commentPostID := ""
	if s.repos.Posts != nil {
		if postID, err := s.repos.Posts.GetCommentPostID(r.Context(), commentID, principal.UserID); err == nil {
			commentPostID = postID
		} else {
			s.logger.WarnContext(r.Context(), "failed to load comment post for realtime delete", "error", err, "comment_id", commentID)
		}
	}
	if err := s.postService.DeleteComment(r.Context(), commentID, principal.UserID); err != nil {
		writePostServiceError(w, r, err)
		return
	}
	if commentPostID != "" {
		s.publishPostSnapshotRealtimeEvent(r.Context(), principal.UserID, commentPostID, "post.comment.deleted", "comment.deleted", map[string]any{
			"commentId": commentID,
		})
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleDeleteReply(w http.ResponseWriter, r *http.Request) {
	if !s.requirePostService(w, r) {
		return
	}
	principal, ok := authPrincipalFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid access token")
		return
	}
	replyID := r.PathValue("replyId")
	if reqErr := validateUUID(replyID, "replyId"); reqErr != nil {
		writeRequestError(w, r, reqErr)
		return
	}
	replyPostID := ""
	if s.repos.Posts != nil {
		if postID, err := s.repos.Posts.GetReplyPostID(r.Context(), replyID, principal.UserID); err == nil {
			replyPostID = postID
		} else {
			s.logger.WarnContext(r.Context(), "failed to load reply post for realtime delete", "error", err, "reply_id", replyID)
		}
	}
	if err := s.postService.DeleteReply(r.Context(), replyID, principal.UserID); err != nil {
		writePostServiceError(w, r, err)
		return
	}
	if replyPostID != "" {
		s.publishPostSnapshotRealtimeEvent(r.Context(), principal.UserID, replyPostID, "post.reply.deleted", "reply.deleted", map[string]any{
			"replyId": replyID,
		})
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func makePostResponses(posts []repository.PostEntry, includeFullMedia bool) []postResponse {
	responses := make([]postResponse, 0, len(posts))
	for _, post := range posts {
		responses = append(responses, makePostResponse(post, includeFullMedia))
	}
	return responses
}

func makePostResponse(entry repository.PostEntry, includeFullMedia bool) postResponse {
	media := makePostMediaResponses(entry.Media)
	response := postResponse{
		PostID:           entry.Post.ID,
		Author:           makePublicUserResponse(entry.Author),
		Title:            entry.Post.Title,
		Content:          entry.Post.Content,
		CoverMediaID:     entry.Post.CoverMediaID,
		MediaPreview:     media,
		LikeCount:        entry.Post.LikeCount,
		CommentCount:     entry.Post.CommentCount,
		ContentCreatedAt: entry.Post.ContentCreatedAt,
		Visibility:       entry.Post.Visibility,
		Viewer: postViewerResponse{
			IsLiked:          entry.Viewer.IsLiked,
			IsFollowedAuthor: entry.Viewer.IsFollowedAuthor,
		},
		Version:   entry.Post.Version,
		ETag:      resourceETag("post", entry.Post.ID, entry.Post.Version),
		CreatedAt: entry.Post.CreatedAt,
		UpdatedAt: entry.Post.UpdatedAt,
	}
	if includeFullMedia {
		response.Media = media
	}
	return response
}

func makePostMediaResponses(media []repository.PostMedia) []postMediaResponse {
	responses := make([]postMediaResponse, 0, len(media))
	for _, item := range media {
		responses = append(responses, postMediaResponse{
			MediaID:     item.ID,
			FileID:      item.FileID,
			MediaType:   item.MediaType,
			SortOrder:   item.SortOrder,
			Width:       item.Width,
			Height:      item.Height,
			DurationMS:  item.DurationMS,
			CoverFileID: item.CoverFileID,
			Playback:    item.Playback,
			LiveStatus:  item.LiveStatus,
			Metadata:    item.Metadata,
		})
	}
	return responses
}

func makePostCommentResponses(comments []repository.PostComment) []postCommentResponse {
	responses := make([]postCommentResponse, 0, len(comments))
	for _, comment := range comments {
		responses = append(responses, makePostCommentResponse(comment))
	}
	return responses
}

func makePostCommentResponse(comment repository.PostComment) postCommentResponse {
	return postCommentResponse{
		CommentID:  comment.ID,
		PostID:     comment.PostID,
		Author:     makePublicUserResponse(comment.Author),
		Content:    comment.Content,
		LikeCount:  comment.LikeCount,
		ReplyCount: comment.ReplyCount,
		Viewer:     commentViewerState{IsLiked: comment.Viewer.IsLiked},
		Replies:    makePostReplyResponses(comment.Replies),
		CreatedAt:  comment.CreatedAt,
		UpdatedAt:  comment.UpdatedAt,
	}
}

func makePostReplyResponses(replies []repository.PostReply) []postReplyResponse {
	responses := make([]postReplyResponse, 0, len(replies))
	for _, reply := range replies {
		responses = append(responses, makePostReplyResponse(reply))
	}
	return responses
}

func makePostReplyResponse(reply repository.PostReply) postReplyResponse {
	var targetUser *userResponse
	if reply.TargetUser != nil {
		response := makePublicUserResponse(*reply.TargetUser)
		targetUser = &response
	}
	return postReplyResponse{
		ReplyID:       reply.ID,
		CommentID:     reply.CommentID,
		PostID:        reply.PostID,
		Author:        makePublicUserResponse(reply.Author),
		TargetUser:    targetUser,
		TargetReplyID: reply.TargetReplyID,
		Content:       reply.Content,
		LikeCount:     reply.LikeCount,
		Viewer:        commentViewerState{IsLiked: reply.Viewer.IsLiked},
		CreatedAt:     reply.CreatedAt,
		UpdatedAt:     reply.UpdatedAt,
	}
}

func makePostMediaInputs(media []postMediaRequest) []postsvc.MediaInput {
	inputs := make([]postsvc.MediaInput, 0, len(media))
	for _, item := range media {
		inputs = append(inputs, postsvc.MediaInput{
			FileID:      item.FileID,
			MediaType:   item.MediaType,
			SortOrder:   item.SortOrder,
			Width:       item.Width,
			Height:      item.Height,
			DurationMS:  item.DurationMS,
			CoverFileID: item.CoverFileID,
			Playback:    item.Playback,
			LiveStatus:  item.LiveStatus,
			Metadata:    item.Metadata,
		})
	}
	return inputs
}

func validatePostMediaRequests(media []postMediaRequest) *requestError {
	for _, item := range media {
		if reqErr := validateUUID(item.FileID, "fileId"); reqErr != nil {
			return reqErr
		}
		if item.CoverFileID != nil {
			if reqErr := validateOptionalUUID(*item.CoverFileID, "coverFileId"); reqErr != nil {
				return reqErr
			}
		}
	}
	return nil
}

func parseOptionalBool(value string, fallback bool, field string) (bool, *requestError) {
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, &requestError{Status: http.StatusBadRequest, Code: "VALIDATION_ERROR", Message: field + " must be a boolean"}
	}
	return parsed, nil
}

func (s *Server) requirePostService(w http.ResponseWriter, r *http.Request) bool {
	if s.postService != nil {
		return true
	}
	writeError(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "Post service is not configured")
	return false
}

func writePostServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, postsvc.ErrValidation):
		writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "Invalid post request")
	case errors.Is(err, postsvc.ErrForbidden):
		writeError(w, r, http.StatusForbidden, "FORBIDDEN", "Forbidden")
	case errors.Is(err, postsvc.ErrConflict):
		writeError(w, r, http.StatusConflict, "CONFLICT", "Post resource conflict")
	case errors.Is(err, postsvc.ErrUnavailable):
		writeError(w, r, http.StatusServiceUnavailable, "SERVICE_NOT_READY", "Post service is not configured")
	case errors.Is(err, repository.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Resource not found")
	default:
		writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Internal server error")
	}
}

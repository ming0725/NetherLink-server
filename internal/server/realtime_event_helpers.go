package server

import (
	"context"
)

func (s *Server) publishSelfRealtimeEvent(ctx context.Context, userID string, eventType string, payload any) {
	s.publishRealtimeEvent(ctx, []string{userID}, eventType, payload)
}

func (s *Server) publishProfileRealtimeEvent(ctx context.Context, userID string, payload any) {
	userIDs := []string{userID}
	if s.repos.Friends != nil {
		friendUserIDs, err := s.repos.Friends.ListActiveFriendUserIDs(ctx, userID)
		if err != nil {
			s.logger.WarnContext(ctx, "failed to list profile realtime recipients", "error", err, "user_id", userID)
		} else {
			userIDs = append(userIDs, friendUserIDs...)
		}
	}
	s.publishRealtimeEvent(ctx, uniqueRealtimeUserIDs(userIDs), "user.profile.updated", payload)
}

func (s *Server) publishGroupRealtimeEvent(ctx context.Context, groupID string, eventType string, payload any) {
	if s.repos.Groups == nil {
		return
	}
	userIDs, err := s.repos.Groups.ListActiveMemberUserIDs(ctx, groupID)
	if err != nil {
		s.logger.WarnContext(ctx, "failed to list group realtime recipients", "error", err, "group_id", groupID, "event_type", eventType)
		return
	}
	s.publishRealtimeEvent(ctx, userIDs, eventType, payload)
}

func (s *Server) publishGroupRealtimeEventTo(ctx context.Context, userIDs []string, eventType string, payload any) {
	s.publishRealtimeEvent(ctx, uniqueRealtimeUserIDs(userIDs), eventType, payload)
}

func (s *Server) publishGroupPrivilegedRealtimeEvent(ctx context.Context, groupID string, eventType string, payload any) {
	if s.repos.Groups == nil {
		return
	}
	userIDs, err := s.repos.Groups.ListPrivilegedMemberUserIDs(ctx, groupID)
	if err != nil {
		s.logger.WarnContext(ctx, "failed to list group privileged realtime recipients", "error", err, "group_id", groupID, "event_type", eventType)
		return
	}
	s.publishRealtimeEvent(ctx, userIDs, eventType, payload)
}

func (s *Server) publishPostSnapshotRealtimeEvent(ctx context.Context, viewerUserID string, postID string, eventType string, reason string, extra map[string]any) {
	if s.repos.Posts == nil {
		return
	}
	entry, err := s.repos.Posts.GetPost(ctx, viewerUserID, postID)
	if err != nil {
		s.logger.WarnContext(ctx, "failed to load post realtime snapshot", "error", err, "post_id", postID, "event_type", eventType)
		return
	}
	payload := map[string]any{
		"reason": reason,
		"post":   makePostResponse(entry, true),
	}
	for key, value := range extra {
		payload[key] = value
	}
	s.publishRealtimeEvent(ctx, uniqueRealtimeUserIDs([]string{viewerUserID, entry.Post.AuthorID}), eventType, payload)
}

func uniqueRealtimeUserIDs(userIDs []string) []string {
	seen := make(map[string]struct{}, len(userIDs))
	unique := make([]string, 0, len(userIDs))
	for _, userID := range userIDs {
		if userID == "" {
			continue
		}
		if _, ok := seen[userID]; ok {
			continue
		}
		seen[userID] = struct{}{}
		unique = append(unique, userID)
	}
	return unique
}

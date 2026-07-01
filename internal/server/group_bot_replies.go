package server

import (
	"context"
	"errors"
	"strings"
	"time"

	aisvc "nlserver/internal/agent/conversation"
	chatsvc "nlserver/internal/chat"
	"nlserver/internal/repository"
)

const groupBotReplyDedupTTL = 30 * time.Minute

func (s *Server) triggerGroupBotReplies(ctx context.Context, message repository.Message) {
	if s == nil || s.aiService == nil || s.chatService == nil {
		return
	}
	agentIDs := s.mentionedGroupBotAgentIDs(ctx, message)
	if len(agentIDs) == 0 {
		return
	}
	for _, agentID := range agentIDs {
		agentID := agentID
		if !s.claimGroupBotReply(message.ID, agentID) {
			continue
		}
		go s.generateAndPublishGroupBotReply(ctx, message, agentID)
	}
}

func (s *Server) generateAndPublishGroupBotReply(parentCtx context.Context, trigger repository.Message, agentID string) {
	timeout := s.cfg.AI.RequestTimeout
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parentCtx), timeout+10*time.Second)
	defer cancel()

	result, err := s.aiService.GenerateGroupBotReply(ctx, aisvcGroupBotInput(agentID, trigger))
	if err != nil {
		if s.logger != nil {
			s.logger.WarnContext(ctx, "failed to generate group bot reply", "error", err, "agent_id", agentID, "message_id", trigger.ID, "conversation_id", trigger.ConversationID)
		}
		return
	}
	referencedMessageID := trigger.ID
	reply, err := s.chatService.SendMessage(ctx, chatsvc.SendMessageInput{
		UserID:              result.Agent.BotUserID,
		ConversationID:      trigger.ConversationID,
		ClientMessageID:     groupBotClientMessageID(trigger.ID, result.Agent.ID),
		Type:                "text",
		Content:             chatsvc.MessageContentInput{Text: result.Content},
		ReferencedMessageID: &referencedMessageID,
	})
	if err != nil {
		if s.logger != nil {
			s.logger.WarnContext(ctx, "failed to send group bot reply", "error", err, "agent_id", agentID, "message_id", trigger.ID, "conversation_id", trigger.ConversationID)
		}
		return
	}
	response, err := s.makeMessageResponseFromContext(ctx, reply)
	if err != nil {
		if s.logger != nil {
			s.logger.WarnContext(ctx, "failed to build group bot reply response", "error", err, "message_id", reply.ID, "conversation_id", reply.ConversationID)
		}
		return
	}
	s.publishConversationMessage(ctx, trigger.ConversationID, "chat.message.created", response)
	s.publishAllConversationSyncStates(ctx, trigger.ConversationID, "message.created")
	s.publishAllConversationUpdated(ctx, trigger.ConversationID, "message.created")
}

func aisvcGroupBotInput(agentID string, trigger repository.Message) aisvc.GroupBotReplyInput {
	return aisvc.GroupBotReplyInput{
		AgentID:          agentID,
		InvokerUserID:    trigger.SenderID,
		ConversationID:   trigger.ConversationID,
		TriggerMessageID: trigger.ID,
	}
}

func (s *Server) mentionedGroupBotAgentIDs(ctx context.Context, message repository.Message) []string {
	identifiers := mentionedAgentIDs(message)
	if len(identifiers) == 0 {
		return nil
	}
	if s == nil || s.repos.Chat == nil || s.repos.Agents == nil {
		return identifiers
	}

	conversation, err := s.repos.Chat.GetConversationByID(ctx, message.ConversationID)
	if err != nil {
		if s.logger != nil {
			s.logger.WarnContext(ctx, "failed to resolve group bot mentions conversation", "error", err, "message_id", message.ID, "conversation_id", message.ConversationID)
		}
		return identifiers
	}
	if conversation.Type != "group" || conversation.GroupID == nil {
		return nil
	}

	seen := map[string]struct{}{}
	agentIDs := make([]string, 0, len(identifiers))
	for _, identifier := range identifiers {
		profile, _, err := s.repos.Agents.GetActiveGroupAgentByIdentifier(ctx, *conversation.GroupID, identifier)
		if err != nil {
			if s.logger != nil && !errors.Is(err, repository.ErrNotFound) {
				s.logger.WarnContext(ctx, "failed to resolve group bot mention", "error", err, "identifier", identifier, "message_id", message.ID, "conversation_id", message.ConversationID)
			}
			continue
		}
		if _, ok := seen[profile.ID]; ok {
			continue
		}
		seen[profile.ID] = struct{}{}
		agentIDs = append(agentIDs, profile.ID)
	}
	return agentIDs
}

func mentionedAgentIDs(message repository.Message) []string {
	seen := map[string]struct{}{}
	values := []string{}
	for _, mention := range message.Mentions {
		if strings.ToLower(strings.TrimSpace(mention.TargetType)) != "ai" {
			continue
		}
		agentID := mentionAgentIdentifier(mention)
		if agentID == "" {
			continue
		}
		if _, ok := seen[agentID]; ok {
			continue
		}
		seen[agentID] = struct{}{}
		values = append(values, agentID)
	}
	return values
}

func mentionAgentIdentifier(mention repository.MessageMention) string {
	if mention.TargetUserID != nil {
		if targetUserID := strings.TrimSpace(*mention.TargetUserID); targetUserID != "" {
			return targetUserID
		}
	}
	return strings.TrimSpace(mention.TargetIdentifier)
}

func (s *Server) claimGroupBotReply(triggerMessageID string, agentID string) bool {
	key := groupBotReplyDedupKey(triggerMessageID, agentID)
	if s == nil || key == "" {
		return false
	}
	now := time.Now()

	s.groupBotRepliesMu.Lock()
	defer s.groupBotRepliesMu.Unlock()

	if s.groupBotRepliesSeen == nil {
		s.groupBotRepliesSeen = make(map[string]time.Time)
	}
	for existingKey, seenAt := range s.groupBotRepliesSeen {
		if now.Sub(seenAt) > groupBotReplyDedupTTL {
			delete(s.groupBotRepliesSeen, existingKey)
		}
	}
	if _, ok := s.groupBotRepliesSeen[key]; ok {
		return false
	}
	s.groupBotRepliesSeen[key] = now
	return true
}

func groupBotReplyDedupKey(triggerMessageID string, agentID string) string {
	triggerMessageID = strings.TrimSpace(triggerMessageID)
	agentID = strings.TrimSpace(agentID)
	if triggerMessageID == "" || agentID == "" {
		return ""
	}
	return triggerMessageID + ":" + agentID
}

func groupBotClientMessageID(triggerMessageID string, agentID string) string {
	return "group-bot:" + strings.TrimSpace(triggerMessageID) + ":" + strings.TrimSpace(agentID)
}

package conversation

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"nlserver/internal/agent/guardrails"
	"nlserver/internal/agent/mcp/mcp_websearch"
	agentmodel "nlserver/internal/agent/model"
	agentprompts "nlserver/internal/agent/prompts"
	agentruntime "nlserver/internal/agent/runtime"
	agenttools "nlserver/internal/agent/tools"
	"nlserver/internal/repository"
)

const (
	groupBotContextMessageLimit  = 36
	groupBotMaxContextChars      = 9000
	groupBotHistoryMessageLimit  = 10
	groupBotHistoryFallbackLimit = 18
	groupBotMaxHistoryChars      = 4000
	groupBotUserMemoryLimit      = 8
	groupBotMaxUserMemoryChars   = 2000
	groupBotSummaryLimit         = 6
	groupBotMaxSummaryChars      = 2600
	groupBotMaxGroupStateChars   = 2600
	groupBotMaxOutputTokens      = 700
	groupBotMaxReplyRunes        = 900
	groupChatSearchContextBefore = 8
	groupChatSearchContextAfter  = 8
	groupChatSearchMaxContext    = 20
)

var (
	markdownLinkPattern       = regexp.MustCompile(`\[([^\]\n]+)\]\((https?://[^)\s]+)\)`)
	markdownLinePrefixPattern = regexp.MustCompile(`(?m)^\s{0,3}(#{1,6}\s+|[-*+]\s+|\d+[\.)]\s+)`)
	sourceSectionPattern      = regexp.MustCompile(`(?is)(?:^|\n)\s*(?:来源|参考来源|参考|引用|source|sources|reference|references|citation|citations)\s*[:：].*$`)
	plainURLPattern           = regexp.MustCompile(`https?://[^\s<>)\]）】]+`)
	groupBotHistoryCuePattern = regexp.MustCompile(`(?i)(上次|之前|以前|昨天|前天|上周|上个月|刚才聊|刚刚聊|聊过|讨论过|说过|提过|记得|remember|last time|earlier|previously|before|yesterday)`)
)

type groupBotPromptMemoryContext struct {
	HistoryTranscript string
	UserMemories      string
	GroupState        string
	Summaries         string
	HistoryMessageIDs []string
	UserMemoryIDs     []string
	GroupStateIDs     []string
	SummaryIDs        []string
}

type GroupBotReplyInput struct {
	AgentID          string
	InvokerUserID    string
	ConversationID   string
	TriggerMessageID string
	UserTimezone     string
}

type GroupBotReplyResult struct {
	Agent   repository.AgentProfile
	Run     repository.AgentRun
	Content string
}

type groupBotToolContext struct {
	BotUserID         string
	GroupID           string
	ConversationID    string
	TriggerMessageSeq int64
	UserTimezone      string
}

func (s *Service) GenerateGroupBotReply(ctx context.Context, input GroupBotReplyInput) (GroupBotReplyResult, error) {
	if s == nil || s.repos.Chat == nil || s.repos.Groups == nil || s.repos.Agents == nil {
		return GroupBotReplyResult{}, ErrUnavailable
	}
	if s.client == nil {
		return GroupBotReplyResult{}, ErrUnavailable
	}
	if strings.TrimSpace(input.AgentID) == "" ||
		strings.TrimSpace(input.InvokerUserID) == "" ||
		strings.TrimSpace(input.ConversationID) == "" ||
		strings.TrimSpace(input.TriggerMessageID) == "" {
		return GroupBotReplyResult{}, ErrValidation
	}
	userTimezone, err := normalizeUserTimezone(input.UserTimezone)
	if err != nil {
		return GroupBotReplyResult{}, err
	}
	conversation, err := s.repos.Chat.GetConversationByID(ctx, input.ConversationID)
	if err != nil {
		return GroupBotReplyResult{}, mapRepositoryError(err)
	}
	if conversation.Type != "group" || conversation.GroupID == nil {
		return GroupBotReplyResult{}, ErrValidation
	}
	groupID := *conversation.GroupID
	if _, err := s.repos.Groups.GetMember(ctx, groupID, input.InvokerUserID); err != nil {
		return GroupBotReplyResult{}, mapRepositoryError(err)
	}
	profile, _, err := s.repos.Agents.GetActiveGroupAgentByIdentifier(ctx, groupID, input.AgentID)
	if err != nil {
		return GroupBotReplyResult{}, mapRepositoryError(err)
	}
	if profile.BotUserID == input.InvokerUserID {
		return GroupBotReplyResult{}, ErrValidation
	}
	trigger, err := s.repos.Chat.GetMessageForParticipant(ctx, profile.BotUserID, input.TriggerMessageID)
	if err != nil {
		return GroupBotReplyResult{}, mapRepositoryError(err)
	}
	if trigger.ConversationID != input.ConversationID || trigger.RecalledAt != nil {
		return GroupBotReplyResult{}, ErrValidation
	}
	group, err := s.repos.Groups.Get(ctx, groupID)
	if err != nil {
		return GroupBotReplyResult{}, mapRepositoryError(err)
	}
	history, err := s.repos.Chat.ListMessagesForParticipant(ctx, profile.BotUserID, "group-bot", input.ConversationID, nil, nil, groupBotContextMessageLimit)
	if err != nil {
		return GroupBotReplyResult{}, mapRepositoryError(err)
	}
	reverseMessages(history)
	triggerText := groupBotMessageText(trigger)
	if strings.TrimSpace(triggerText) == "" {
		triggerText = "[非文本消息]"
	}
	retrievedHistory := s.retrieveGroupBotHistory(ctx, profile.BotUserID, input.ConversationID, trigger, triggerText, userTimezone, history)
	displayNameMessages := append([]repository.Message{trigger}, history...)
	displayNameMessages = append(displayNameMessages, retrievedHistory...)
	displayNames := s.groupBotDisplayNames(ctx, profile.BotUserID, groupID, displayNameMessages)
	transcript, contextMessageIDs := formatGroupBotTranscript(history, trigger.ID, displayNames)
	historyTranscript, historyMessageIDs := formatGroupBotTranscript(retrievedHistory, "", displayNames)
	if len(retrievedHistory) == 0 {
		historyTranscript = ""
	}
	memoryContext := s.loadGroupBotMemoryContext(ctx, profile, group, input.ConversationID, trigger.SenderID)
	memoryContext.HistoryTranscript = historyTranscript
	memoryContext.HistoryMessageIDs = historyMessageIDs

	model := s.cfg.DefaultModel
	if profile.Model != nil && strings.TrimSpace(*profile.Model) != "" {
		model = strings.TrimSpace(*profile.Model)
	}
	model, err = normalizeAIModel("", model)
	if err != nil {
		return GroupBotReplyResult{}, err
	}
	thinkingType, err := normalizeThinkingType("", s.cfg.ThinkingType)
	if err != nil {
		return GroupBotReplyResult{}, err
	}
	reasoningEffort, err := normalizeReasoningEffort("", s.cfg.ReasoningEffort)
	if err != nil {
		return GroupBotReplyResult{}, err
	}
	now := groupBotNow(userTimezone)
	messages := []agentmodel.ModelMessage{
		{
			Role:    "system",
			Content: agentprompts.GroupBotSystemPrompt(profile.Name, model, now, profile.BasePrompt),
		},
		{
			Role:    "user",
			Content: formatGroupBotUserPromptWithMemory(group.Name, displayNames[trigger.SenderID], triggerText, transcript, memoryContext),
		},
	}
	inputSnapshot, _ := json.Marshal(map[string]any{
		"conversationId":    input.ConversationID,
		"groupId":           groupID,
		"triggerMessageId":  trigger.ID,
		"triggerMessageSeq": trigger.MessageSeq,
		"triggerSenderId":   trigger.SenderID,
		"triggerText":       triggerText,
	})
	contextRefs, _ := json.Marshal(map[string]any{
		"conversationId":    input.ConversationID,
		"groupId":           groupID,
		"messageIds":        contextMessageIDs,
		"historyMessageIds": memoryContext.HistoryMessageIDs,
		"userMemoryIds":     memoryContext.UserMemoryIDs,
		"groupStateIds":     memoryContext.GroupStateIDs,
		"summaryIds":        memoryContext.SummaryIDs,
	})
	invokerUserID := input.InvokerUserID
	sourceID := trigger.ID
	run, err := s.repos.Agents.CreateRun(ctx, repository.CreateAgentRunParams{
		AgentID:       profile.ID,
		InvokerUserID: &invokerUserID,
		SourceType:    "group_message",
		SourceID:      &sourceID,
		InputSnapshot: inputSnapshot,
		ContextRefs:   contextRefs,
	})
	if err != nil {
		return GroupBotReplyResult{}, mapRepositoryError(err)
	}

	started := time.Now()
	toolAudits := []agentruntime.ToolAuditEvent{}
	runtime := agentruntime.NewRuntime(chatClientModelGateway{client: s.client})
	toolContext := groupBotToolContext{
		BotUserID:         profile.BotUserID,
		GroupID:           groupID,
		ConversationID:    input.ConversationID,
		TriggerMessageSeq: trigger.MessageSeq,
		UserTimezone:      userTimezone,
	}
	groupBotToolExecutor := func(ctx context.Context, call agenttools.ToolCall) string {
		return s.runGroupBotCompletionTool(ctx, call, toolContext)
	}
	result, err := runtime.Complete(ctx, agentruntime.Request{
		RunID:            run.ID,
		AgentID:          profile.ID,
		InvokerUserID:    input.InvokerUserID,
		ScopeType:        "group",
		ScopeID:          groupID,
		UserTimezone:     userTimezone,
		Model:            model,
		Messages:         messages,
		Tools:            groupBotCompletionToolDefinitions(s.cfg),
		ToolExecutor:     groupBotToolExecutor,
		ToolPolicy:       groupBotToolPolicy(profile.ToolPolicy),
		SafetyPolicy:     groupBotSafetyPolicy(profile.SafetyPolicy),
		ThinkingType:     thinkingType,
		ReasoningEffort:  reasoningEffort,
		ModelTimeout:     s.cfg.RequestTimeout,
		RoutingMaxTokens: toolRoutingMaxTokens,
		MaxOutputTokens:  groupBotOutputTokens(s.cfg.MaxOutputTokens),
		MaxIterations:    maxToolRoutingIterations,
		MaxToolCalls:     maxCompletionToolCalls,
		OnToolAudit: func(event agentruntime.ToolAuditEvent) error {
			toolAudits = append(toolAudits, event)
			return nil
		},
	})
	latencyMS := int(time.Since(started).Milliseconds())
	if err != nil {
		errorCode := "GENERATION_FAILED"
		_, _ = s.finishGroupBotRun(ctx, run.ID, "failed", toolAudits, map[string]any{"error": err.Error()}, &model, 0, 0, latencyMS, &errorCode)
		return GroupBotReplyResult{}, err
	}
	content := sanitizeGroupBotReplyText(result.Completion.Content)
	if strings.TrimSpace(content) == "" {
		content = fallbackGroupBotReply(triggerText)
	}
	completionTokens := result.Completion.TokenCount
	if completionTokens == 0 {
		completionTokens = roughTokenCount(content)
	}
	updatedRun, updateErr := s.finishGroupBotRun(ctx, run.ID, "completed", toolAudits, map[string]any{
		"content":      content,
		"finishReason": result.Completion.FinishReason,
		"usedTools":    result.UsedTools,
	}, &model, 0, completionTokens, latencyMS, nil)
	if updateErr == nil {
		run = updatedRun
	}
	return GroupBotReplyResult{
		Agent:   profile,
		Run:     run,
		Content: content,
	}, nil
}

func (s *Service) finishGroupBotRun(ctx context.Context, runID string, status string, toolAudits []agentruntime.ToolAuditEvent, output map[string]any, model *string, promptTokens int, completionTokens int, latencyMS int, errorCode *string) (repository.AgentRun, error) {
	toolCalls, _ := json.Marshal(toolAudits)
	outputJSON, _ := json.Marshal(output)
	return s.repos.Agents.UpdateRunResult(ctx, repository.UpdateAgentRunResultParams{
		RunID:            runID,
		Status:           status,
		ToolCalls:        toolCalls,
		Output:           outputJSON,
		Model:            model,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		LatencyMS:        latencyMS,
		ErrorCode:        errorCode,
	})
}

func (s *Service) runGroupBotCompletionTool(ctx context.Context, call agenttools.ToolCall, toolContext groupBotToolContext) string {
	if call.Name != toolNameGroupChatSearch {
		executor := func(ctx context.Context, nested agenttools.ToolCall) string {
			return s.runGroupBotCompletionTool(ctx, nested, toolContext)
		}
		return s.runCompletionToolWithExecutor(ctx, call, executor)
	}
	payload, err := s.runGroupChatSearchTool(ctx, call.Arguments, toolContext)
	if err != nil {
		s.logCompletionToolFailure(ctx, call, payload, err)
		payload = map[string]any{
			"ok":    false,
			"error": err.Error(),
			"tool":  toolNameGroupChatSearch,
		}
	} else if toolPayloadFailed(payload) {
		s.logCompletionToolFailure(ctx, call, payload, nil)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return `{"ok":false,"error":"failed to encode tool result"}`
	}
	return string(data)
}

func (s *Service) runGroupChatSearchTool(ctx context.Context, args any, toolContext groupBotToolContext) (map[string]any, error) {
	if s == nil || s.repos.Chat == nil {
		return nil, ErrUnavailable
	}
	if strings.TrimSpace(toolContext.BotUserID) == "" ||
		strings.TrimSpace(toolContext.GroupID) == "" ||
		strings.TrimSpace(toolContext.ConversationID) == "" {
		return nil, fmt.Errorf("group_chat_search requires current group bot context")
	}
	var input groupChatSearchInput
	if err := mcp_websearch.MapToolArgs(args, &input); err != nil {
		return nil, err
	}
	mode := strings.ToLower(strings.TrimSpace(input.Mode))
	if mode == "" {
		mode = "search"
	}
	if mode == "context" {
		return s.runGroupChatContextTool(ctx, input, toolContext)
	}
	if mode != "search" {
		return nil, fmt.Errorf("group_chat_search mode %q is invalid", input.Mode)
	}
	input.Query = strings.TrimSpace(input.Query)
	if input.Query == "" {
		return nil, fmt.Errorf("group_chat_search query is required")
	}
	if input.Limit <= 0 {
		input.Limit = groupBotHistoryMessageLimit
	}
	if input.Limit > 20 {
		input.Limit = 20
	}
	keywords := uniqueStrings(input.Keywords)
	if len(keywords) == 0 {
		keywords = groupBotHistoryKeywords(input.Query)
	}
	since, until, err := groupChatSearchTimeBounds(input, toolContext.UserTimezone)
	if err != nil {
		return nil, err
	}
	if since == nil && until == nil {
		since, until = groupBotHistoryTimeWindow(input.Query, toolContext.UserTimezone)
	}
	beforeSeq := toolContext.TriggerMessageSeq
	if input.BeforeMessageSeq > 0 && (beforeSeq <= 0 || input.BeforeMessageSeq < beforeSeq) {
		beforeSeq = input.BeforeMessageSeq
	}
	var beforeSeqPtr *int64
	if beforeSeq > 0 {
		beforeSeqPtr = &beforeSeq
	}
	messages, err := s.repos.Chat.SearchMessagesForParticipant(ctx, toolContext.BotUserID, "group-bot", toolContext.ConversationID, beforeSeqPtr, keywords, since, until, input.Limit)
	if err != nil {
		return nil, mapRepositoryError(err)
	}
	reverseMessages(messages)
	displayNames := s.groupBotDisplayNames(ctx, toolContext.BotUserID, toolContext.GroupID, messages)
	results := groupChatSearchResults(messages, displayNames)
	status := "group_chat_records"
	if len(results) == 0 {
		status = "insufficient_no_group_chat_matches"
	}
	return map[string]any{
		"ok":              len(results) > 0,
		"tool":            toolNameGroupChatSearch,
		"mode":            "search",
		"query":           input.Query,
		"keywords":        keywords,
		"since":           formatOptionalRFC3339(since),
		"until":           formatOptionalRFC3339(until),
		"before_seq":      beforeSeq,
		"result_count":    len(results),
		"result_type":     "search_results",
		"evidence_status": status,
		"evidence_note":   "These are private group-chat records visible to the group bot. They can verify what group members said, but they do not verify external public facts.",
		"next_action":     "If a hit may be relevant but needs surrounding context, call group_chat_search again with mode=context and that result's message_id or message_seq. Expand only the most relevant hit first.",
		"results":         results,
	}, nil
}

func (s *Service) runGroupChatContextTool(ctx context.Context, input groupChatSearchInput, toolContext groupBotToolContext) (map[string]any, error) {
	beforeSeq := toolContext.TriggerMessageSeq
	if input.BeforeMessageSeq > 0 && (beforeSeq <= 0 || input.BeforeMessageSeq < beforeSeq) {
		beforeSeq = input.BeforeMessageSeq
	}
	target, err := s.resolveGroupChatContextTarget(ctx, input, toolContext, beforeSeq)
	if err != nil {
		return nil, err
	}
	before, after := groupChatContextWindowSizes(input.ContextBefore, input.ContextAfter)
	window, err := s.groupChatContextWindow(ctx, toolContext, target, beforeSeq, before, after)
	if err != nil {
		return nil, err
	}
	displayNames := s.groupBotDisplayNames(ctx, toolContext.BotUserID, toolContext.GroupID, window)
	contextPayload, contextText := groupChatSearchContextPayload(window, target.ID, displayNames)
	status := "group_chat_context"
	if len(contextPayload) == 0 {
		status = "insufficient_no_group_chat_context"
	}
	targetPayload, _ := groupChatSearchMessagePayload(target, displayNames, true)
	return map[string]any{
		"ok":              len(contextPayload) > 0,
		"tool":            toolNameGroupChatSearch,
		"mode":            "context",
		"query":           strings.TrimSpace(input.Query),
		"message_id":      target.ID,
		"message_seq":     target.MessageSeq,
		"before_seq":      beforeSeq,
		"context_window":  map[string]any{"before": before, "after": after},
		"result_count":    len(contextPayload),
		"result_type":     "message_context",
		"evidence_status": status,
		"evidence_note":   "These are private group-chat records near the selected message. Use them to resolve pronouns and local conversational references.",
		"target_message":  targetPayload,
		"context":         contextPayload,
		"context_text":    contextText,
	}, nil
}

func groupChatSearchTimeBounds(input groupChatSearchInput, userTimezone string) (*time.Time, *time.Time, error) {
	since, err := parseGroupChatSearchTime(input.Since, userTimezone, false)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid since: %w", err)
	}
	until, err := parseGroupChatSearchTime(input.Until, userTimezone, true)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid until: %w", err)
	}
	if since != nil && until != nil && since.After(*until) {
		return nil, nil, fmt.Errorf("since must not be after until")
	}
	return since, until, nil
}

func parseGroupChatSearchTime(value string, userTimezone string, exclusiveDateEnd bool) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	location := time.Local
	if strings.TrimSpace(userTimezone) != "" {
		loaded, err := time.LoadLocation(userTimezone)
		if err != nil {
			return nil, err
		}
		location = loaded
	}
	if len(value) == len("2006-01-02") {
		parsed, err := time.ParseInLocation("2006-01-02", value, location)
		if err != nil {
			return nil, err
		}
		if exclusiveDateEnd {
			parsed = parsed.AddDate(0, 0, 1)
		}
		return &parsed, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, err
	}
	parsed = parsed.In(location)
	return &parsed, nil
}

func formatOptionalRFC3339(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.Format(time.RFC3339)
}

func (s *Service) resolveGroupChatContextTarget(ctx context.Context, input groupChatSearchInput, toolContext groupBotToolContext, beforeSeq int64) (repository.Message, error) {
	messageID := strings.TrimSpace(input.MessageID)
	if messageID != "" {
		message, err := s.repos.Chat.GetMessageForParticipant(ctx, toolContext.BotUserID, messageID)
		if err != nil {
			return repository.Message{}, mapRepositoryError(err)
		}
		if message.ConversationID != toolContext.ConversationID || message.RecalledAt != nil {
			return repository.Message{}, ErrValidation
		}
		if beforeSeq > 0 && message.MessageSeq >= beforeSeq {
			return repository.Message{}, fmt.Errorf("group_chat_search context target must be before the triggering message")
		}
		visible, err := s.groupChatMessageBySeq(ctx, toolContext, message.MessageSeq)
		if err != nil {
			return repository.Message{}, err
		}
		if visible.ID != message.ID {
			return repository.Message{}, ErrValidation
		}
		return visible, nil
	}
	if input.MessageSeq <= 0 {
		return repository.Message{}, fmt.Errorf("group_chat_search context requires message_id or message_seq")
	}
	afterSeq := input.MessageSeq - 1
	if afterSeq < 0 {
		afterSeq = 0
	}
	upperExclusive := input.MessageSeq + 1
	if beforeSeq > 0 && upperExclusive > beforeSeq {
		return repository.Message{}, fmt.Errorf("group_chat_search context target must be before the triggering message")
	}
	messages, err := s.repos.Chat.ListMessagesForParticipant(ctx, toolContext.BotUserID, "group-bot", toolContext.ConversationID, &upperExclusive, &afterSeq, 1)
	if err != nil {
		return repository.Message{}, mapRepositoryError(err)
	}
	if len(messages) == 0 {
		return repository.Message{}, ErrValidation
	}
	return messages[0], nil
}

func (s *Service) groupChatMessageBySeq(ctx context.Context, toolContext groupBotToolContext, messageSeq int64) (repository.Message, error) {
	if messageSeq <= 0 {
		return repository.Message{}, ErrValidation
	}
	afterSeq := messageSeq - 1
	if afterSeq < 0 {
		afterSeq = 0
	}
	upperExclusive := messageSeq + 1
	messages, err := s.repos.Chat.ListMessagesForParticipant(ctx, toolContext.BotUserID, "group-bot", toolContext.ConversationID, &upperExclusive, &afterSeq, 1)
	if err != nil {
		return repository.Message{}, mapRepositoryError(err)
	}
	if len(messages) == 0 {
		return repository.Message{}, ErrValidation
	}
	return messages[0], nil
}

func (s *Service) groupChatContextWindow(ctx context.Context, toolContext groupBotToolContext, target repository.Message, beforeSeq int64, before int, after int) ([]repository.Message, error) {
	window := []repository.Message{target}
	if target.MessageSeq <= 0 {
		return window, nil
	}
	lowerExclusive := target.MessageSeq - int64(before) - 1
	if lowerExclusive < 0 {
		lowerExclusive = 0
	}
	upperExclusive := target.MessageSeq + int64(after) + 1
	if beforeSeq > 0 && upperExclusive > beforeSeq {
		upperExclusive = beforeSeq
	}
	limit := before + after + 1
	loaded, err := s.repos.Chat.ListMessagesForParticipant(ctx, toolContext.BotUserID, "group-bot", toolContext.ConversationID, &upperExclusive, &lowerExclusive, limit)
	if err != nil {
		return nil, mapRepositoryError(err)
	}
	reverseMessages(loaded)
	return ensureGroupChatSearchMatchInContext(loaded, target), nil
}

func groupChatContextWindowSizes(before int, after int) (int, int) {
	if before < 0 {
		before = 0
	}
	if after < 0 {
		after = 0
	}
	if before == 0 && after == 0 {
		before = groupChatSearchContextBefore
		after = groupChatSearchContextAfter
	}
	if before > groupChatSearchMaxContext {
		before = groupChatSearchMaxContext
	}
	if after > groupChatSearchMaxContext {
		after = groupChatSearchMaxContext
	}
	return before, after
}

func ensureGroupChatSearchMatchInContext(window []repository.Message, match repository.Message) []repository.Message {
	if len(window) == 0 {
		return []repository.Message{match}
	}
	for _, message := range window {
		if message.ID == match.ID {
			return window
		}
	}
	window = append(window, match)
	reverseMessages(window)
	return window
}

func groupChatSearchResults(messages []repository.Message, names map[string]string) []map[string]any {
	results := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		result, ok := groupChatSearchMessagePayload(message, names, false)
		if !ok {
			continue
		}
		results = append(results, result)
	}
	return results
}

func groupChatSearchContextPayload(messages []repository.Message, matchID string, names map[string]string) ([]map[string]any, string) {
	context := make([]map[string]any, 0, len(messages))
	lines := make([]string, 0, len(messages))
	for _, message := range messages {
		isMatch := message.ID == matchID
		payload, ok := groupChatSearchMessagePayload(message, names, isMatch)
		if !ok {
			continue
		}
		context = append(context, payload)
		name := strings.TrimSpace(stringValue(payload["sender_name"]))
		text := strings.TrimSpace(stringValue(payload["text"]))
		if name == "" || text == "" {
			continue
		}
		prefix := ""
		if isMatch {
			prefix = "[match] "
		}
		lines = append(lines, fmt.Sprintf("%s%s: %s", prefix, name, text))
	}
	return context, strings.Join(lines, "\n")
}

func groupChatSearchMessagePayload(message repository.Message, names map[string]string, isMatch bool) (map[string]any, bool) {
	if message.RecalledAt != nil {
		return nil, false
	}
	text := groupBotMessageText(message)
	if strings.TrimSpace(text) == "" {
		return nil, false
	}
	senderName := strings.TrimSpace(names[message.SenderID])
	if senderName == "" {
		senderName = message.SenderID
	}
	payload := map[string]any{
		"message_id":  message.ID,
		"message_seq": message.MessageSeq,
		"sender_id":   message.SenderID,
		"sender_name": senderName,
		"created_at":  message.CreatedAt.Format(time.RFC3339),
		"text":        truncateRunes(text, 800),
	}
	if isMatch {
		payload["is_match"] = true
	}
	return payload, true
}

func stringValue(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

func (s *Service) groupBotDisplayNames(ctx context.Context, viewerUserID string, groupID string, messages []repository.Message) map[string]string {
	userIDs := make([]string, 0, len(messages))
	for _, message := range messages {
		userIDs = append(userIDs, message.SenderID)
	}
	userIDs = uniqueStrings(userIDs)
	names := map[string]string{}
	if len(userIDs) == 0 {
		return names
	}
	if s.repos.Users != nil {
		if users, err := s.repos.Users.BatchGetByIDs(ctx, userIDs); err == nil {
			for _, user := range users {
				name := strings.TrimSpace(user.Nick)
				if name == "" {
					name = strings.TrimSpace(user.PublicID)
				}
				if name == "" {
					name = user.ID
				}
				names[user.ID] = name
			}
		}
	}
	if members, err := s.repos.Groups.BatchGetMembers(ctx, viewerUserID, groupID, userIDs); err == nil {
		for _, member := range members {
			if member.Nickname != nil && strings.TrimSpace(*member.Nickname) != "" {
				names[member.User.ID] = strings.TrimSpace(*member.Nickname)
			}
		}
	}
	for _, userID := range userIDs {
		if strings.TrimSpace(names[userID]) == "" {
			names[userID] = userID
		}
	}
	return names
}

func (s *Service) retrieveGroupBotHistory(ctx context.Context, botUserID string, conversationID string, trigger repository.Message, triggerText string, userTimezone string, recent []repository.Message) []repository.Message {
	if s == nil || s.repos.Chat == nil || !groupBotShouldRetrieveHistory(triggerText) {
		return nil
	}
	beforeSeq := trigger.MessageSeq
	for _, message := range recent {
		if message.MessageSeq > 0 && message.MessageSeq < beforeSeq {
			beforeSeq = message.MessageSeq
		}
	}
	keywords := groupBotHistoryKeywords(triggerText)
	since, until := groupBotHistoryTimeWindow(triggerText, userTimezone)
	messages, err := s.repos.Chat.SearchMessagesForParticipant(ctx, botUserID, "group-bot", conversationID, &beforeSeq, keywords, since, until, groupBotHistoryMessageLimit)
	if err != nil {
		s.warnGroupBotMemory(ctx, "failed to retrieve group bot history memory", err)
		return nil
	}
	if len(messages) == 0 && len(keywords) > 0 {
		messages, err = s.repos.Chat.SearchMessagesForParticipant(ctx, botUserID, "group-bot", conversationID, &beforeSeq, nil, since, until, groupBotHistoryFallbackLimit)
		if err != nil {
			s.warnGroupBotMemory(ctx, "failed to retrieve group bot history fallback", err)
			return nil
		}
	}
	reverseMessages(messages)
	return messages
}

func (s *Service) loadGroupBotMemoryContext(ctx context.Context, profile repository.AgentProfile, group repository.Group, conversationID string, triggerUserID string) groupBotPromptMemoryContext {
	context := groupBotPromptMemoryContext{}
	context.UserMemories, context.UserMemoryIDs = s.formatGroupBotUserMemories(ctx, profile.ID, group.ID, triggerUserID)
	context.GroupState, context.GroupStateIDs = s.formatGroupBotGroupState(ctx, group)
	context.Summaries, context.SummaryIDs = s.formatGroupBotSummaries(ctx, group.ID, conversationID)
	return context
}

func (s *Service) formatGroupBotUserMemories(ctx context.Context, agentID string, groupID string, userID string) (string, []string) {
	if s == nil || s.repos.Agents == nil || strings.TrimSpace(userID) == "" {
		return "", nil
	}
	memories, err := s.repos.Agents.ListGroupBotMemories(ctx, agentID, groupID, userID, groupBotUserMemoryLimit)
	if err != nil {
		s.warnGroupBotMemory(ctx, "failed to load group bot user memories", err)
		return "", nil
	}
	lines := []string{}
	ids := []string{}
	for _, memory := range memories {
		content := strings.TrimSpace(memory.Content)
		if content == "" {
			continue
		}
		memoryType := strings.TrimSpace(memory.MemoryType)
		if memoryType == "" {
			memoryType = "memory"
		}
		lines = append(lines, fmt.Sprintf("- %s: %s", memoryType, content))
		ids = append(ids, memory.ID)
	}
	return trimContextLinesFromStart(lines, groupBotMaxUserMemoryChars), ids
}

func (s *Service) formatGroupBotGroupState(ctx context.Context, group repository.Group) (string, []string) {
	lines := []string{}
	if group.Introduction != nil && strings.TrimSpace(*group.Introduction) != "" {
		lines = append(lines, "- introduction: "+strings.TrimSpace(*group.Introduction))
	}
	if group.Announcement != nil && strings.TrimSpace(*group.Announcement) != "" {
		lines = append(lines, "- announcement: "+strings.TrimSpace(*group.Announcement))
	}
	ids := []string{}
	if s != nil && s.repos.Groups != nil {
		states, err := s.repos.Groups.ListStates(ctx, group.ID, []string{"members", "rules", "topics", "active_topics", "votes", "polls", "vote_results", "decisions"})
		if err != nil {
			s.warnGroupBotMemory(ctx, "failed to load group bot group state", err)
		} else {
			for _, state := range states {
				rendered := compactJSONForPrompt(state.State)
				if strings.TrimSpace(rendered) == "" || rendered == "{}" || rendered == "[]" {
					continue
				}
				stateType := strings.TrimSpace(state.StateType)
				if stateType == "" {
					stateType = "state"
				}
				lines = append(lines, fmt.Sprintf("- %s: %s", stateType, rendered))
				ids = append(ids, state.ID)
			}
		}
	}
	return trimContextLinesFromStart(lines, groupBotMaxGroupStateChars), ids
}

func (s *Service) formatGroupBotSummaries(ctx context.Context, groupID string, conversationID string) (string, []string) {
	if s == nil || s.repos.Groups == nil {
		return "", nil
	}
	summaries, err := s.repos.Groups.ListChatSummaries(ctx, groupID, conversationID, []string{"day", "week", "month"}, groupBotSummaryLimit)
	if err != nil {
		s.warnGroupBotMemory(ctx, "failed to load group bot summaries", err)
		return "", nil
	}
	lines := []string{}
	ids := []string{}
	for _, summary := range summaries {
		content := strings.TrimSpace(summary.Summary)
		if content == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s %s~%s: %s", summary.PeriodType, summary.PeriodStart, summary.PeriodEnd, content))
		ids = append(ids, summary.ID)
	}
	return trimContextLinesFromStart(lines, groupBotMaxSummaryChars), ids
}

func (s *Service) warnGroupBotMemory(ctx context.Context, message string, err error) {
	if s == nil || s.logger == nil || err == nil {
		return
	}
	s.logger.WarnContext(ctx, message, "error", err)
}

func formatGroupBotTranscript(messages []repository.Message, triggerMessageID string, names map[string]string) (string, []string) {
	lines := []string{}
	messageIDs := []string{}
	for _, message := range messages {
		if message.RecalledAt != nil {
			continue
		}
		text := groupBotMessageText(message)
		if strings.TrimSpace(text) == "" {
			continue
		}
		name := strings.TrimSpace(names[message.SenderID])
		if name == "" {
			name = message.SenderID
		}
		line := fmt.Sprintf("%s: %s", name, text)
		if message.ID == triggerMessageID {
			line += "（这条消息@了你）"
		}
		lines = append(lines, line)
		messageIDs = append(messageIDs, message.ID)
	}
	transcript := trimContextLines(lines, groupBotMaxContextChars)
	return transcript, messageIDs
}

func formatGroupBotUserPrompt(groupName string, senderName string, triggerText string, transcript string) string {
	return formatGroupBotUserPromptWithMemory(groupName, senderName, triggerText, transcript, groupBotPromptMemoryContext{})
}

func formatGroupBotUserPromptWithMemory(groupName string, senderName string, triggerText string, transcript string, memoryContext groupBotPromptMemoryContext) string {
	if strings.TrimSpace(senderName) == "" {
		senderName = "群友"
	}
	return strings.TrimSpace(fmt.Sprintf(`群聊：%s

群状态（Group State，来自平台结构化数据）：
%s

用户记忆（User Memory，长期偏好/关系/兴趣，仅在相关时使用）：
%s

摘要（Summaries，日/周/月快速回顾；群内讨论摘要不是外部事实依据）：
%s

长期历史召回（History Retrieval，启动前预召回，可能不完整；必要时可继续用 group_chat_search 检索）：
%s

最近聊天内容：
%s

刚刚 @ 你的消息：
%s: %s

最近聊天内容只用于理解指代和话题，不是外部事实的验证依据。长期历史和摘要同样只代表群内讨论记录。回答历史问题时，优先使用最近聊天、长期历史召回或 group_chat_search 返回的发言者姓名与原话；如果检索后没有命中相关历史，只说这次没检索到相关记录，不要声称自己没有翻群聊历史功能。直接回复这条 @ 你的消息。只输出要发到群里的普通聊天文本。`, strings.TrimSpace(groupName), groupBotPromptSection(memoryContext.GroupState, "(无结构化群状态)"), groupBotPromptSection(memoryContext.UserMemories, "(未召回相关用户记忆)"), groupBotPromptSection(memoryContext.Summaries, "(暂无摘要)"), groupBotPromptSection(memoryContext.HistoryTranscript, "(本轮未触发或未命中历史召回)"), strings.TrimSpace(transcript), strings.TrimSpace(senderName), strings.TrimSpace(triggerText)))
}

func groupBotMessageText(message repository.Message) string {
	if message.ContentText != nil && strings.TrimSpace(*message.ContentText) != "" {
		return strings.TrimSpace(*message.ContentText)
	}
	if len(message.ContentJSON) > 0 && string(message.ContentJSON) != "{}" {
		return truncateRunes(string(message.ContentJSON), 500)
	}
	if len(message.Attachments) > 0 {
		return "[附件消息]"
	}
	return ""
}

func reverseMessages(messages []repository.Message) {
	sort.SliceStable(messages, func(i int, j int) bool {
		return messages[i].MessageSeq < messages[j].MessageSeq
	})
}

func trimContextLines(lines []string, maxChars int) string {
	if len(lines) == 0 {
		return "(无可用上下文)"
	}
	kept := []string{}
	total := 0
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		lineLen := len([]rune(line)) + 1
		if maxChars > 0 && total+lineLen > maxChars && len(kept) > 0 {
			break
		}
		kept = append(kept, line)
		total += lineLen
	}
	for i, j := 0, len(kept)-1; i < j; i, j = i+1, j-1 {
		kept[i], kept[j] = kept[j], kept[i]
	}
	return strings.Join(kept, "\n")
}

func trimContextLinesFromStart(lines []string, maxChars int) string {
	kept := []string{}
	total := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lineLen := len([]rune(line)) + 1
		if maxChars > 0 && total+lineLen > maxChars && len(kept) > 0 {
			break
		}
		kept = append(kept, line)
		total += lineLen
	}
	return strings.Join(kept, "\n")
}

func groupBotPromptSection(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func groupBotShouldRetrieveHistory(text string) bool {
	return groupBotHistoryCuePattern.MatchString(strings.TrimSpace(text))
}

func groupBotHistoryKeywords(text string) []string {
	normalized := strings.TrimSpace(text)
	if normalized == "" {
		return nil
	}
	replacer := strings.NewReplacer(
		"上次", " ",
		"之前", " ",
		"以前", " ",
		"昨天", " ",
		"前天", " ",
		"上周", " ",
		"上个月", " ",
		"刚才", " ",
		"刚刚", " ",
		"聊过", " ",
		"讨论过", " ",
		"说过", " ",
		"提过", " ",
		"记得", " ",
		"那个", " ",
		"这个", " ",
		"一下", " ",
		"什么", " ",
		"怎么", " ",
		"为什么", " ",
	)
	normalized = replacer.Replace(normalized)
	var fields []string
	var current strings.Builder
	for _, r := range normalized {
		if unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.Is(unicode.Han, r) {
			current.WriteRune(r)
			continue
		}
		if current.Len() > 0 {
			fields = append(fields, current.String())
			current.Reset()
		}
	}
	if current.Len() > 0 {
		fields = append(fields, current.String())
	}
	stopWords := map[string]struct{}{
		"the": {}, "and": {}, "for": {}, "that": {}, "this": {}, "with": {}, "you": {}, "your": {}, "about": {},
		"remember": {}, "before": {}, "earlier": {}, "previously": {}, "yesterday": {}, "last": {}, "time": {},
	}
	keywords := []string{}
	seen := map[string]struct{}{}
	addKeyword := func(value string) {
		value = strings.TrimSpace(value)
		if len([]rune(value)) < 2 {
			return
		}
		if len([]rune(value)) > 24 {
			value = truncateRunes(value, 24)
		}
		lower := strings.ToLower(value)
		if _, ok := stopWords[lower]; ok {
			return
		}
		if _, ok := seen[lower]; ok {
			return
		}
		seen[lower] = struct{}{}
		keywords = append(keywords, value)
	}
	for _, field := range fields {
		field = strings.TrimSpace(field)
		lower := strings.ToLower(field)
		if _, ok := stopWords[lower]; ok {
			continue
		}
		if !containsWeakHanCue(field) {
			addKeyword(field)
		}
		for _, keyword := range groupBotHanKeywords(field) {
			addKeyword(keyword)
			if len(keywords) >= 8 {
				break
			}
		}
		if len(keywords) >= 6 {
			break
		}
	}
	return keywords
}

func groupBotHanKeywords(value string) []string {
	runes := []rune(value)
	keywords := []string{}
	for start := 0; start < len(runes); start++ {
		if !unicode.Is(unicode.Han, runes[start]) {
			continue
		}
		for length := 2; length <= 4 && start+length <= len(runes); length++ {
			candidate := string(runes[start : start+length])
			if containsWeakHanCue(candidate) {
				continue
			}
			keywords = append(keywords, candidate)
		}
	}
	return keywords
}

func containsWeakHanCue(value string) bool {
	for _, r := range value {
		switch r {
		case '谁', '我', '你', '他', '她', '它', '们', '的', '了', '呢', '吗', '么', '啊', '呀', '吧', '被', '把', '和', '或', '与', '是', '有', '在', '过', '问', '下', '个', '这', '那', '啥':
			return true
		}
	}
	return false
}

func groupBotHistoryTimeWindow(text string, userTimezone string) (*time.Time, *time.Time) {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return nil, nil
	}
	offsetDays := 0
	switch {
	case strings.Contains(lower, "前天"):
		offsetDays = -2
	case strings.Contains(lower, "昨天") || strings.Contains(lower, "yesterday"):
		offsetDays = -1
	default:
		return nil, nil
	}
	now := groupBotNow(userTimezone)
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, offsetDays)
	end := start.AddDate(0, 0, 1)
	return &start, &end
}

func compactJSONForPrompt(raw json.RawMessage) string {
	raw = jsonOrEmpty(raw)
	if len(raw) == 0 {
		return ""
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return truncateRunes(strings.TrimSpace(string(raw)), 1200)
	}
	data, err := json.Marshal(value)
	if err != nil {
		return truncateRunes(strings.TrimSpace(string(raw)), 1200)
	}
	return truncateRunes(string(data), 1200)
}

func jsonOrEmpty(raw json.RawMessage) json.RawMessage {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	return json.RawMessage(trimmed)
}

func groupBotNow(userTimezone string) time.Time {
	now := time.Now()
	if strings.TrimSpace(userTimezone) == "" {
		return now
	}
	location, err := time.LoadLocation(userTimezone)
	if err != nil {
		return now
	}
	return now.In(location)
}

func groupBotOutputTokens(configured int) int {
	if configured > 0 && configured < groupBotMaxOutputTokens {
		return configured
	}
	return groupBotMaxOutputTokens
}

func groupBotToolPolicy(raw json.RawMessage) guardrails.ToolPolicy {
	var policy guardrails.ToolPolicy
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &policy)
	}
	if len(policy.AllowedTools) > 0 {
		policy.AllowedTools = uniqueTrimmedStrings(append(policy.AllowedTools, toolNameGroupChatSearch))
	}
	return policy
}

func groupBotSafetyPolicy(raw json.RawMessage) guardrails.SafetyPolicy {
	policy := guardrails.SafetyPolicy{RequireConfirmationForSideEffects: true}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &policy)
	}
	policy.RequireConfirmationForSideEffects = true
	return policy
}

func sanitizeGroupBotReplyText(value string) string {
	text := strings.TrimSpace(value)
	if text == "" {
		return ""
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	filteredLines := []string{}
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			continue
		}
		filteredLines = append(filteredLines, line)
	}
	text = strings.Join(filteredLines, "\n")
	text = sourceSectionPattern.ReplaceAllString(text, "")
	text = markdownLinkPattern.ReplaceAllString(text, "$1")
	text = plainURLPattern.ReplaceAllString(text, "")
	text = markdownLinePrefixPattern.ReplaceAllString(text, "")
	replacer := strings.NewReplacer(
		"**", "",
		"__", "",
		"`", "",
		"> ", "",
		"（）", "",
		"()", "",
		"：。", "。",
		":.", ".",
	)
	text = replacer.Replace(text)
	text = strings.Join(strings.Fields(text), " ")
	text = strings.TrimSpace(text)
	return truncateRunes(text, groupBotMaxReplyRunes)
}

func fallbackGroupBotReply(triggerText string) string {
	if groupBotPrefersChinese(triggerText) {
		return "我这边没生成出有用回复，稍后再试一下。"
	}
	return "I couldn't produce a useful reply this time. Try me again in a bit."
}

func groupBotPrefersChinese(text string) bool {
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

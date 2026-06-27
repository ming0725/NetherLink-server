package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"nlserver/internal/agent/guardrails"
	agentmodel "nlserver/internal/agent/model"
	agentprompts "nlserver/internal/agent/prompts"
	agentruntime "nlserver/internal/agent/runtime"
	"nlserver/internal/agent/tools"
	"nlserver/internal/config"
	"nlserver/internal/repository"
)

var (
	ErrValidation  = errors.New("ai: validation failed")
	ErrForbidden   = errors.New("ai: forbidden")
	ErrNotReady    = errors.New("ai: file not ready")
	ErrParseFailed = errors.New("ai: file parse failed")
	ErrUnavailable = errors.New("ai: unavailable")
)

const (
	maxAIMessageLength = 20000
	maxAITitleLength   = 120
	maxAIModelLength   = 128
	maxAIContextChunks = 8
	maxAIContextChars  = 12000
	temporaryFileTTL   = 24 * time.Hour

	maxToolRoutingIterations = 5
	toolRoutingMaxTokens     = 256
	maxCompletionToolCalls   = 8
	defaultAIResponseTokens  = 4096
	maxTraceContextEntries   = 6
	maxTraceContextChars     = 6000
)

var aiModelPattern = regexp.MustCompile(`^[A-Za-z0-9._:/@+-]{1,128}$`)

type Service struct {
	cfg    config.AIConfig
	repos  repository.Repositories
	tx     repository.TxBeginner
	client ChatClient
	logger *slog.Logger
}

type CreateConversationInput struct {
	UserID       string
	Title        *string
	FirstMessage *string
	Model        string
}

type UpdateConversationTitleInput struct {
	UserID         string
	ConversationID string
	Title          string
}

type SendMessageInput struct {
	UserID          string
	ConversationID  string
	Message         string
	ClientMessageID string
	AIFileIDs       []string
	Model           string
	ThinkingType    string
	ReasoningEffort string
	UserTimezone    string
}

type SendMessageResult struct {
	UserMessage        repository.AIMessage
	AssistantMessageID string
	StreamID           string
	UsedFiles          []repository.AIUploadedFile
	Title              *string
}

type StreamMessageInput struct {
	SendMessageInput
	OnDelta func(StreamMessageDelta) error
}

type StreamMessageDelta struct {
	Content      string
	Kind         string
	Thinking     bool
	Progress     *StreamProgress
	Transient    bool
	StepID       string
	SegmentID    string
	SegmentStart bool
	SegmentEnd   bool
}

type StreamProgress struct {
	StepID   string
	Sequence int
	Phase    string
	Status   string
	Message  string
	Tool     string
	Query    string
	URLs     []string
}

type StreamMessageResult struct {
	UserMessage      repository.AIMessage
	AssistantMessage repository.AIMessage
	UsedFiles        []repository.AIUploadedFile
	FinishReason     string
	Title            *string
}

type BindFileInput struct {
	UserID          string
	ConversationID  string
	FileID          string
	Purpose         string
	RetentionPolicy *string
}

func NewService(cfg config.AIConfig, repos repository.Repositories, tx repository.TxBeginner, client ChatClient) *Service {
	return &Service{cfg: cfg, repos: repos, tx: tx, client: client}
}

func (s *Service) SetLogger(logger *slog.Logger) {
	if s == nil {
		return
	}
	s.logger = logger
}

func (s *Service) ListConversations(ctx context.Context, userID string, limit int, offset int) ([]repository.AIConversation, error) {
	if s.repos.AI == nil {
		return nil, ErrUnavailable
	}
	return s.repos.AI.ListConversations(ctx, userID, limit, offset)
}

func (s *Service) CreateConversation(ctx context.Context, input CreateConversationInput) (repository.AIConversation, error) {
	if s.repos.AI == nil {
		return repository.AIConversation{}, ErrUnavailable
	}
	title := normalizeOptionalTitle(input.Title)
	if title != nil && titleTooLong(*title) {
		return repository.AIConversation{}, ErrValidation
	}
	firstMessage := strings.TrimSpace(valueOrEmpty(input.FirstMessage))
	if firstMessage != "" && len(firstMessage) > maxAIMessageLength {
		return repository.AIConversation{}, ErrValidation
	}
	if title == nil && firstMessage != "" {
		fallbackTitle := fallbackConversationTitle(firstMessage)
		title = &fallbackTitle
	}
	model, err := normalizeAIModel(input.Model, s.cfg.DefaultModel)
	if err != nil {
		return repository.AIConversation{}, err
	}

	var conversation repository.AIConversation
	if s.tx != nil {
		err := repository.WithTx(ctx, s.tx, func(repos repository.Repositories) error {
			created, err := repos.AI.CreateConversation(ctx, repository.CreateAIConversationParams{
				UserID:           input.UserID,
				Title:            title,
				Model:            model,
				ContextMaxTokens: s.cfg.MaxContextTokens,
			})
			if err != nil {
				return err
			}
			if firstMessage != "" {
				if _, err := repos.AI.CreateMessage(ctx, repository.CreateAIMessageParams{
					ConversationID: created.ID,
					Role:           "user",
					Text:           firstMessage,
					TokenCount:     roughTokenCount(firstMessage),
				}); err != nil {
					return err
				}
			}
			conversation = created
			return nil
		})
		return conversation, mapRepositoryError(err)
	}

	created, err := s.repos.AI.CreateConversation(ctx, repository.CreateAIConversationParams{
		UserID:           input.UserID,
		Title:            title,
		Model:            model,
		ContextMaxTokens: s.cfg.MaxContextTokens,
	})
	if err != nil {
		return repository.AIConversation{}, mapRepositoryError(err)
	}
	if firstMessage != "" {
		if _, err := s.repos.AI.CreateMessage(ctx, repository.CreateAIMessageParams{
			ConversationID: created.ID,
			Role:           "user",
			Text:           firstMessage,
			TokenCount:     roughTokenCount(firstMessage),
		}); err != nil {
			return repository.AIConversation{}, mapRepositoryError(err)
		}
	}
	return created, nil
}

func (s *Service) UpdateConversationTitle(ctx context.Context, input UpdateConversationTitleInput) (repository.AIConversation, error) {
	if s.repos.AI == nil {
		return repository.AIConversation{}, ErrUnavailable
	}
	title := strings.TrimSpace(input.Title)
	if strings.TrimSpace(input.UserID) == "" || strings.TrimSpace(input.ConversationID) == "" || title == "" || titleTooLong(title) {
		return repository.AIConversation{}, ErrValidation
	}
	conversation, err := s.repos.AI.UpdateConversationTitle(ctx, input.UserID, input.ConversationID, title)
	if err != nil {
		return repository.AIConversation{}, mapRepositoryError(err)
	}
	return conversation, nil
}

func (s *Service) ListMessages(ctx context.Context, userID string, conversationID string) ([]repository.AIMessage, error) {
	if s.repos.AI == nil {
		return nil, ErrUnavailable
	}
	if _, err := s.repos.AI.GetConversation(ctx, userID, conversationID); err != nil {
		return nil, mapRepositoryError(err)
	}
	return s.listMessagesWithTraces(ctx, userID, conversationID)
}

func (s *Service) DeleteConversation(ctx context.Context, userID string, conversationID string) error {
	if s.repos.AI == nil {
		return ErrUnavailable
	}
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(conversationID) == "" {
		return ErrValidation
	}
	return mapRepositoryError(s.repos.AI.DeleteConversation(ctx, userID, conversationID))
}

func (s *Service) SendMessage(ctx context.Context, input SendMessageInput) (SendMessageResult, error) {
	prepared, err := s.prepareMessageCompletion(ctx, input)
	if err != nil {
		return SendMessageResult{}, err
	}
	completion, trace, err := s.completeMessageWithTools(ctx, prepared)
	if err != nil {
		return SendMessageResult{}, err
	}
	assistantMessage, err := s.saveAssistantCompletion(ctx, prepared, completion, trace)
	if err != nil {
		return SendMessageResult{}, err
	}
	title := s.finishFirstRoundTitle(ctx, prepared, assistantMessage)

	return SendMessageResult{
		UserMessage:        prepared.userMessage,
		AssistantMessageID: assistantMessage.ID,
		StreamID:           "",
		UsedFiles:          prepared.usedFiles,
		Title:              title,
	}, nil
}

func (s *Service) StreamMessage(ctx context.Context, input StreamMessageInput) (StreamMessageResult, error) {
	prepared, err := s.prepareMessageCompletion(ctx, input.SendMessageInput)
	if err != nil {
		return StreamMessageResult{}, err
	}
	result, err := s.runAgentStream(ctx, prepared, input.OnDelta)
	completion := result.Completion
	if err != nil {
		if isStreamCancellation(ctx, err) {
			result, saveErr := s.saveCancelledStreamMessage(ctx, prepared, completion, result.Trace)
			if saveErr != nil {
				return StreamMessageResult{}, saveErr
			}
			return result, context.Canceled
		}
		return StreamMessageResult{}, err
	}
	return s.saveCompletedStreamMessage(ctx, prepared, completion, result.Trace)
}

type agentCompletionResult struct {
	Completion CompletionResponse
	Messages   []ChatMessage
	UsedTools  bool
	Trace      *conversationTrace
}

func (s *Service) runAgentCompletion(ctx context.Context, prepared preparedMessageCompletion) (agentCompletionResult, error) {
	traceCollector := newConversationTraceCollector()
	runtime := agentruntime.NewRuntime(chatClientModelGateway{client: s.client})
	result, err := runtime.Complete(ctx, s.agentRuntimeRequest(prepared, traceCollector.Report))
	if err != nil {
		return agentCompletionResult{}, err
	}
	return agentCompletionResult{
		Completion: fromModelResponse(result.Completion),
		Messages:   fromModelMessages(result.Messages),
		UsedTools:  result.UsedTools,
		Trace:      traceCollector.Trace("completed"),
	}, nil
}

func (s *Service) runAgentStream(ctx context.Context, prepared preparedMessageCompletion, onDelta func(StreamMessageDelta) error) (agentCompletionResult, error) {
	traceCollector := newConversationTraceCollector()
	lastTransientProgressKey := ""
	transientSegment := 0
	reportProgress := func(progress agentruntime.Progress) error {
		traceCollector.Add(progress)
		if onDelta == nil {
			return nil
		}
		if text := strings.TrimSpace(progress.Narration); text != "" {
			progressKey := strings.TrimSpace(progress.StepID) + "\x00" + text
			if progressKey != lastTransientProgressKey {
				lastTransientProgressKey = progressKey
				transientSegment++
				if err := emitTransientNarration(onDelta, text+"\n", progress.StepID, fmt.Sprintf("progress-%d", transientSegment)); err != nil {
					return streamDeliveryError{err: err}
				}
			}
		}
		if err := onDelta(StreamMessageDelta{
			Thinking: true,
			Progress: &StreamProgress{
				StepID:   progress.StepID,
				Sequence: progress.Sequence,
				Phase:    progress.Phase,
				Status:   progress.Status,
				Message:  progress.Message,
				Tool:     progress.Tool,
				Query:    progress.Query,
				URLs:     progress.URLs,
			},
		}); err != nil {
			return streamDeliveryError{err: err}
		}
		return nil
	}
	runtime := agentruntime.NewRuntime(chatClientModelGateway{client: s.client})
	result, err := runtime.Stream(ctx, s.agentRuntimeRequest(prepared, reportProgress), func(delta agentmodel.ModelDelta) error {
		if onDelta == nil || delta.Content == "" {
			return nil
		}
		for _, content := range splitStreamDeltas(delta.Content, 24) {
			if err := onDelta(StreamMessageDelta{
				Content:   content,
				Kind:      "answer",
				Transient: false,
			}); err != nil {
				return streamDeliveryError{err: err}
			}
		}
		return nil
	})
	return agentCompletionResult{
		Completion: fromModelResponse(result.Completion),
		Messages:   fromModelMessages(result.Messages),
		UsedTools:  result.UsedTools,
		Trace:      traceCollector.Trace(traceStatusForError(ctx, err)),
	}, err
}

func emitTransientNarration(onDelta func(StreamMessageDelta) error, text string, stepID string, segmentID string) error {
	chunks := splitNarrationDeltas(text, 3)
	for index, chunk := range chunks {
		if err := onDelta(StreamMessageDelta{
			Content:      chunk,
			Kind:         "progress",
			Transient:    true,
			StepID:       stepID,
			SegmentID:    segmentID,
			SegmentStart: index == 0,
			SegmentEnd:   index == len(chunks)-1,
		}); err != nil {
			return err
		}
	}
	return nil
}

func splitStreamDeltas(text string, maxRunes int) []string {
	if text == "" {
		return nil
	}
	return splitNarrationDeltas(text, maxRunes)
}

func splitNarrationDeltas(text string, maxRunes int) []string {
	if maxRunes <= 0 {
		maxRunes = 1
	}
	runes := []rune(text)
	chunks := make([]string, 0, (len(runes)+maxRunes-1)/maxRunes)
	for start := 0; start < len(runes); start += maxRunes {
		end := start + maxRunes
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[start:end]))
	}
	return chunks
}

type conversationTrace struct {
	Status     string
	Summary    string
	Steps      []conversationTraceStep
	SourceRefs []string
}

type conversationTraceStep struct {
	Sequence  int       `json:"sequence"`
	StepID    string    `json:"stepId,omitempty"`
	Phase     string    `json:"phase"`
	Status    string    `json:"status"`
	Text      string    `json:"text"`
	Tool      string    `json:"tool,omitempty"`
	Query     string    `json:"query,omitempty"`
	URLs      []string  `json:"urls,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

type conversationTraceCollector struct {
	steps       []conversationTraceStep
	stepIndexes map[string]int
}

func newConversationTraceCollector() *conversationTraceCollector {
	return &conversationTraceCollector{stepIndexes: map[string]int{}}
}

func (c *conversationTraceCollector) Report(progress agentruntime.Progress) error {
	c.Add(progress)
	return nil
}

func (c *conversationTraceCollector) Add(progress agentruntime.Progress) {
	if c == nil {
		return
	}
	stepID := strings.TrimSpace(progress.StepID)
	text := strings.TrimSpace(progress.Narration)
	if stepID != "" {
		if index, ok := c.stepIndexes[stepID]; ok {
			step := &c.steps[index]
			if progress.Sequence > 0 {
				step.Sequence = progress.Sequence
			}
			if phase := strings.TrimSpace(progress.Phase); phase != "" {
				step.Phase = phase
			}
			if status := strings.TrimSpace(progress.Status); status != "" {
				step.Status = status
			}
			if text != "" {
				step.Text = text
			}
			if tool := strings.TrimSpace(progress.Tool); tool != "" {
				step.Tool = tool
			}
			if query := strings.TrimSpace(progress.Query); query != "" {
				step.Query = query
			}
			step.URLs = uniqueTrimmedStrings(append(step.URLs, progress.URLs...))
			return
		}
	}
	if text == "" {
		return
	}
	sequence := progress.Sequence
	if sequence <= 0 {
		sequence = len(c.steps) + 1
	}
	step := conversationTraceStep{
		Sequence:  sequence,
		StepID:    stepID,
		Phase:     strings.TrimSpace(progress.Phase),
		Status:    strings.TrimSpace(progress.Status),
		Text:      text,
		Tool:      strings.TrimSpace(progress.Tool),
		Query:     strings.TrimSpace(progress.Query),
		URLs:      uniqueTrimmedStrings(progress.URLs),
		CreatedAt: time.Now(),
	}
	c.steps = append(c.steps, step)
	if stepID != "" {
		if c.stepIndexes == nil {
			c.stepIndexes = map[string]int{}
		}
		c.stepIndexes[stepID] = len(c.steps) - 1
	}
}

func (c *conversationTraceCollector) Trace(status string) *conversationTrace {
	if c == nil || len(c.steps) == 0 {
		return nil
	}
	steps := make([]conversationTraceStep, len(c.steps))
	copy(steps, c.steps)
	return &conversationTrace{
		Status:     normalizeTraceStatus(status),
		Summary:    summarizeConversationTrace(steps),
		Steps:      steps,
		SourceRefs: traceSourceRefs(steps),
	}
}

func summarizeConversationTrace(steps []conversationTraceStep) string {
	parts := make([]string, 0, len(steps))
	for _, step := range steps {
		switch {
		case step.Tool == toolNameWebSearch && step.Status == "completed" && step.Query != "":
			parts = append(parts, "检索了："+step.Query)
		case step.Tool == toolNameWebRead && step.Status == "completed" && step.Query != "":
			parts = append(parts, "读取并核对了："+step.Query)
		}
	}
	if len(parts) == 0 {
		for _, step := range steps {
			if text := strings.TrimSpace(step.Text); text != "" {
				parts = append(parts, text)
			}
		}
	}
	return truncateRunes(strings.Join(uniqueTrimmedStrings(parts), "；"), 2000)
}

func traceSourceRefs(steps []conversationTraceStep) []string {
	values := []string{}
	for _, step := range steps {
		values = append(values, step.URLs...)
	}
	return uniqueTrimmedStrings(values)
}

func traceStatusForError(ctx context.Context, err error) string {
	if err == nil {
		return "completed"
	}
	if isStreamCancellation(ctx, err) {
		return "cancelled"
	}
	return "failed"
}

func normalizeTraceStatus(status string) string {
	switch strings.TrimSpace(status) {
	case "cancelled", "failed":
		return strings.TrimSpace(status)
	default:
		return "completed"
	}
}

func uniqueTrimmedStrings(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		result = append(result, trimmed)
	}
	return result
}

func truncateRunes(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return strings.TrimSpace(string(runes[:limit])) + "..."
}

func (s *Service) agentRuntimeRequest(prepared preparedMessageCompletion, report agentruntime.ProgressReporter) agentruntime.Request {
	toolDefinitions := completionToolDefinitions(s.cfg)
	return agentruntime.Request{
		Model:            prepared.model,
		UserTimezone:     prepared.userTimezone,
		Messages:         toModelMessages(prepared.messages),
		Tools:            toolDefinitions,
		ToolExecutor:     s.runCompletionTool,
		ToolPolicy:       completionToolPolicy(toolDefinitions),
		SafetyPolicy:     guardrails.SafetyPolicy{RequireConfirmationForSideEffects: true},
		ThinkingType:     prepared.thinkingType,
		ReasoningEffort:  prepared.reasoningEffort,
		ModelTimeout:     s.cfg.RequestTimeout,
		RoutingMaxTokens: toolRoutingMaxTokens,
		MaxOutputTokens:  normalizedAIResponseTokens(s.cfg.MaxOutputTokens),
		MaxIterations:    maxToolRoutingIterations,
		MaxToolCalls:     maxCompletionToolCalls,
		OnProgress:       report,
	}
}

func completionToolPolicy(definitions []tools.ToolDefinition) guardrails.ToolPolicy {
	allowed := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		if name := strings.TrimSpace(definition.Name); name != "" {
			allowed = append(allowed, name)
		}
	}
	return guardrails.ToolPolicy{AllowedTools: uniqueTrimmedStrings(allowed)}
}

type chatClientModelGateway struct {
	client ChatClient
}

func (g chatClientModelGateway) Complete(ctx context.Context, req agentmodel.ModelRequest) (agentmodel.ModelResponse, error) {
	if g.client == nil {
		return agentmodel.ModelResponse{}, ErrProviderUnavailable
	}
	response, err := g.client.Complete(ctx, CompletionRequest{
		Model:           req.Model,
		Messages:        fromModelMessages(req.Messages),
		Tools:           req.Tools,
		ResponseSchema:  req.ResponseSchema,
		ThinkingType:    req.ThinkingType,
		ReasoningEffort: req.ReasoningEffort,
		MaxOutputTokens: req.MaxOutputTokens,
	})
	if err != nil {
		return agentmodel.ModelResponse{}, err
	}
	return toModelResponse(response), nil
}

func (g chatClientModelGateway) Stream(ctx context.Context, req agentmodel.ModelRequest, onDelta func(agentmodel.ModelDelta) error) (agentmodel.ModelResponse, error) {
	if g.client == nil {
		return agentmodel.ModelResponse{}, ErrProviderUnavailable
	}
	response, err := g.client.Stream(ctx, CompletionRequest{
		Model:           req.Model,
		Messages:        fromModelMessages(req.Messages),
		Tools:           req.Tools,
		ResponseSchema:  req.ResponseSchema,
		ThinkingType:    req.ThinkingType,
		ReasoningEffort: req.ReasoningEffort,
		MaxOutputTokens: req.MaxOutputTokens,
	}, func(delta CompletionStreamDelta) error {
		if onDelta == nil || (!delta.Thinking && delta.Content == "") {
			return nil
		}
		return onDelta(agentmodel.ModelDelta{
			Content:      delta.Content,
			Thinking:     delta.Thinking,
			FinishReason: delta.FinishReason,
		})
	})
	return toModelResponse(response), err
}

func toModelResponse(response CompletionResponse) agentmodel.ModelResponse {
	return agentmodel.ModelResponse{
		Content:      response.Content,
		ToolCalls:    response.ToolCalls,
		TokenCount:   response.TokenCount,
		FinishReason: response.FinishReason,
	}
}

func fromModelResponse(response agentmodel.ModelResponse) CompletionResponse {
	return CompletionResponse{
		Content:      response.Content,
		ToolCalls:    response.ToolCalls,
		TokenCount:   response.TokenCount,
		FinishReason: response.FinishReason,
	}
}

func fromModelMessages(messages []agentmodel.ModelMessage) []ChatMessage {
	result := make([]ChatMessage, 0, len(messages))
	for _, message := range messages {
		result = append(result, ChatMessage{
			Role:       message.Role,
			Content:    message.Content,
			ToolCallID: message.ToolCallID,
			ToolCalls:  message.ToolCalls,
		})
	}
	return result
}

func (s *Service) saveCompletedStreamMessage(ctx context.Context, prepared preparedMessageCompletion, completion CompletionResponse, trace *conversationTrace) (StreamMessageResult, error) {
	assistantMessage, err := s.saveAssistantCompletion(ctx, prepared, completion, trace)
	if err != nil {
		return StreamMessageResult{}, err
	}
	title := s.finishFirstRoundTitle(ctx, prepared, assistantMessage)
	return StreamMessageResult{
		UserMessage:      prepared.userMessage,
		AssistantMessage: assistantMessage,
		UsedFiles:        prepared.usedFiles,
		FinishReason:     completion.FinishReason,
		Title:            title,
	}, nil
}

func (s *Service) saveAssistantCompletion(ctx context.Context, prepared preparedMessageCompletion, completion CompletionResponse, trace *conversationTrace) (repository.AIMessage, error) {
	content := strings.TrimSpace(completion.Content)
	tokenCount := completion.TokenCount
	if tokenCount == 0 {
		tokenCount = roughTokenCount(content)
	}
	create := func(repos repository.Repositories) (repository.AIMessage, error) {
		assistantMessage, err := repos.AI.CreateMessage(ctx, repository.CreateAIMessageParams{
			ConversationID: prepared.conversation.ID,
			Role:           "assistant",
			Text:           content,
			TokenCount:     tokenCount,
		})
		if err != nil {
			return repository.AIMessage{}, err
		}
		if trace != nil {
			savedTrace, err := createRepositoryMessageTrace(ctx, repos.AI, prepared.conversation.ID, assistantMessage.ID, trace)
			if err != nil {
				return repository.AIMessage{}, err
			}
			assistantMessage.Trace = &savedTrace
		}
		return assistantMessage, nil
	}
	if s.tx != nil {
		var assistantMessage repository.AIMessage
		err := repository.WithTx(ctx, s.tx, func(repos repository.Repositories) error {
			created, err := create(repos)
			if err != nil {
				return err
			}
			assistantMessage = created
			return nil
		})
		return assistantMessage, mapRepositoryError(err)
	}
	assistantMessage, err := create(s.repos)
	return assistantMessage, mapRepositoryError(err)
}

func createRepositoryMessageTrace(ctx context.Context, repo *repository.AIRepository, conversationID string, messageID string, trace *conversationTrace) (repository.AIMessageTrace, error) {
	if repo == nil || trace == nil || len(trace.Steps) == 0 {
		return repository.AIMessageTrace{}, nil
	}
	steps, err := json.Marshal(trace.Steps)
	if err != nil {
		return repository.AIMessageTrace{}, err
	}
	sourceRefs, err := json.Marshal(trace.SourceRefs)
	if err != nil {
		return repository.AIMessageTrace{}, err
	}
	return repo.CreateMessageTrace(ctx, repository.CreateAIMessageTraceParams{
		ConversationID: conversationID,
		MessageID:      messageID,
		Status:         trace.Status,
		Summary:        trace.Summary,
		Steps:          steps,
		SourceRefs:     sourceRefs,
	})
}

func (s *Service) saveCancelledStreamMessage(ctx context.Context, prepared preparedMessageCompletion, completion CompletionResponse, trace *conversationTrace) (StreamMessageResult, error) {
	result := StreamMessageResult{
		UserMessage:  prepared.userMessage,
		UsedFiles:    prepared.usedFiles,
		FinishReason: "cancelled",
	}
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	content := strings.TrimSpace(completion.Content)
	if content == "" {
		result.Title = s.finishFirstRoundTitleFromText(persistCtx, prepared, "")
		return result, nil
	}
	tokenCount := completion.TokenCount
	if tokenCount == 0 {
		tokenCount = roughTokenCount(content)
	}
	assistantMessage, err := s.saveAssistantCompletion(persistCtx, prepared, CompletionResponse{
		Content:      content,
		TokenCount:   tokenCount,
		FinishReason: "cancelled",
	}, trace)
	if err != nil {
		return StreamMessageResult{}, err
	}
	result.AssistantMessage = assistantMessage
	result.Title = s.finishFirstRoundTitle(persistCtx, prepared, assistantMessage)
	return result, nil
}

func (s *Service) completeMessageWithTools(ctx context.Context, prepared preparedMessageCompletion) (CompletionResponse, *conversationTrace, error) {
	result, err := s.runAgentCompletion(ctx, prepared)
	if err != nil {
		return CompletionResponse{}, nil, err
	}
	completion, err := s.verifyCompletionIfNeeded(ctx, prepared, result.Messages, result.Completion)
	if err != nil {
		return CompletionResponse{}, nil, err
	}
	return completion, result.Trace, nil
}

func (s *Service) prepareToolAugmentedMessages(ctx context.Context, prepared preparedMessageCompletion) ([]ChatMessage, bool, *CompletionResponse, error) {
	return s.prepareToolAugmentedMessagesMode(ctx, prepared, false, nil)
}

type toolProgressReporter func(phase string, message string, tool string) error

func (s *Service) prepareToolAugmentedMessagesMode(ctx context.Context, prepared preparedMessageCompletion, routeOnly bool, report toolProgressReporter) ([]ChatMessage, bool, *CompletionResponse, error) {
	var progress agentruntime.ProgressReporter
	if report != nil {
		progress = func(progress agentruntime.Progress) error {
			return report(progress.Phase, progress.Message, progress.Tool)
		}
	}
	runtime := agentruntime.NewRuntime(chatClientModelGateway{client: s.client})
	result, completion, err := runtime.Route(ctx, s.agentRuntimeRequest(prepared, progress), routeOnly)
	if err != nil {
		return nil, false, nil, err
	}
	var aiCompletion *CompletionResponse
	if completion != nil {
		converted := fromModelResponse(*completion)
		aiCompletion = &converted
	}
	return fromModelMessages(result.Messages), result.UsedTools, aiCompletion, nil
}

func appendBackendToolResults(messages []ChatMessage, toolResultSummaries []string) []ChatMessage {
	if len(toolResultSummaries) == 0 {
		return messages
	}
	return append(messages, ChatMessage{
		Role:    "system",
		Content: agentprompts.BackendToolEvidencePrompt(toolResultSummaries),
	})
}

func cloneChatMessages(messages []ChatMessage) []ChatMessage {
	cloned := make([]ChatMessage, len(messages))
	copy(cloned, messages)
	return cloned
}

type answerVerificationDecision struct {
	NeedVerify bool   `json:"need_verify"`
	Query      string `json:"query"`
	Topic      string `json:"topic"`
	TargetDate string `json:"target_date"`
	Reason     string `json:"reason"`
}

func (s *Service) verifyCompletionIfNeeded(ctx context.Context, prepared preparedMessageCompletion, messages []ChatMessage, completion CompletionResponse) (CompletionResponse, error) {
	if (strings.TrimSpace(s.cfg.TavilyAPIKey) == "" && strings.TrimSpace(s.cfg.BraveSearchAPIKey) == "") ||
		strings.TrimSpace(completion.Content) == "" {
		return completion, nil
	}
	searchBackedDraft := messagesContainToolEvidence(messages, toolNameWebSearch)
	decision, ok := s.planAnswerVerification(ctx, prepared, completion.Content)
	if !ok && !searchBackedDraft {
		return completion, nil
	}
	if !ok {
		decision = answerVerificationDecision{
			NeedVerify: true,
			Query:      strings.TrimSpace(prepared.userMessage.Text),
			Topic:      "general",
			Reason:     "The draft used web-search context, but the verification planner did not return a valid decision.",
		}
	}
	if !decision.NeedVerify && !searchBackedDraft {
		return completion, nil
	}
	decision.NeedVerify = true
	searchQuery := strings.TrimSpace(decision.Query)
	if searchQuery == "" {
		searchQuery = strings.TrimSpace(prepared.userMessage.Text)
	}
	searchArgs := map[string]any{
		"query":       searchQuery,
		"topic":       decision.Topic,
		"target_date": decision.TargetDate,
		"max_results": 5,
	}
	if prepared.userTimezone != "" {
		searchArgs["timezone"] = prepared.userTimezone
	}
	searchResult := s.runCompletionTool(ctx, tools.ToolCall{
		ID:        "backend_fact_check_search",
		Name:      toolNameWebSearch,
		Arguments: searchArgs,
	})
	readResult := ""
	if sourceURLs := webSearchResultURLs(searchResult, 3); len(sourceURLs) > 0 {
		readResult = s.runCompletionTool(ctx, tools.ToolCall{
			ID:   "backend_fact_check_read",
			Name: toolNameWebRead,
			Arguments: map[string]any{
				"urls":              sourceURLs,
				"query":             searchQuery,
				"extract_depth":     "advanced",
				"format":            "markdown",
				"chunks_per_source": 5,
			},
		})
	}
	verifyContext := fmt.Sprintf("Fact checker decision:\nneed_verify: %t\nreason: %s\nquery: %s\ntopic: %s\ntarget_date: %s\n\nTool %s result:\n%s", decision.NeedVerify, strings.TrimSpace(decision.Reason), searchQuery, decision.Topic, decision.TargetDate, toolNameWebSearch, searchResult)
	if readResult != "" {
		verifyContext += fmt.Sprintf("\n\nTool %s result:\n%s", toolNameWebRead, readResult)
	}
	verifiedMessages := appendBackendToolResults(cloneChatMessages(messages), []string{verifyContext})
	verifiedMessages = append(verifiedMessages, ChatMessage{
		Role:    "system",
		Content: "Revise the draft using only claims supported for the same entity, metric, and date. Remove exact values that are merely predicted, inferred, stale, or contradicted. If evidence_status is insufficient or sources do not explicitly establish the claim, say it could not be verified. Cite only URLs that support the retained claims.",
	})
	verifiedMessages = append(verifiedMessages, ChatMessage{
		Role:    "assistant",
		Content: strings.TrimSpace(completion.Content),
	})

	providerCtx, cancel := timeoutContext(ctx, s.cfg.RequestTimeout)
	defer cancel()
	verified, err := s.client.Complete(providerCtx, CompletionRequest{
		Model:           prepared.model,
		Messages:        verifiedMessages,
		ThinkingType:    "disabled",
		ReasoningEffort: "low",
		MaxOutputTokens: normalizedAIResponseTokens(s.cfg.MaxOutputTokens),
	})
	if err != nil {
		return CompletionResponse{}, err
	}
	if strings.TrimSpace(verified.Content) == "" {
		return completion, nil
	}
	return verified, nil
}

func normalizedAIResponseTokens(value int) int {
	if value > 0 {
		return value
	}
	return defaultAIResponseTokens
}

func webSearchResultURLs(searchResult string, limit int) []string {
	if limit <= 0 {
		return nil
	}
	var decoded struct {
		Results []struct {
			URL string `json:"url"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(searchResult), &decoded); err != nil {
		return nil
	}
	values := make([]string, 0, len(decoded.Results))
	for _, result := range decoded.Results {
		values = append(values, result.URL)
	}
	values = normalizePublicWebURLs(values)
	if len(values) > limit {
		values = values[:limit]
	}
	return values
}

func messagesContainText(messages []ChatMessage, needle string) bool {
	for _, message := range messages {
		if strings.Contains(message.Content, needle) {
			return true
		}
	}
	return false
}

func messagesContainToolEvidence(messages []ChatMessage, toolName string) bool {
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		return false
	}
	if messagesContainText(messages, "Tool "+toolName+" result") ||
		messagesContainText(messages, `"tool":"`+toolName+`"`) {
		return true
	}
	callIDs := map[string]struct{}{}
	for _, message := range messages {
		for _, call := range message.ToolCalls {
			if call.Name == toolName && strings.TrimSpace(call.ID) != "" {
				callIDs[call.ID] = struct{}{}
			}
		}
	}
	for _, message := range messages {
		if message.Role != "tool" || strings.TrimSpace(message.Content) == "" {
			continue
		}
		if _, ok := callIDs[message.ToolCallID]; ok {
			return true
		}
	}
	return false
}

func (s *Service) planAnswerVerification(ctx context.Context, prepared preparedMessageCompletion, draft string) (answerVerificationDecision, bool) {
	providerCtx, cancel := timeoutContext(ctx, s.cfg.RequestTimeout)
	defer cancel()
	response, err := s.client.Complete(providerCtx, CompletionRequest{
		Model: prepared.model,
		Messages: []ChatMessage{
			{Role: "system", Content: answerVerificationSystemPrompt()},
			{Role: "user", Content: formatAnswerVerificationInput(prepared.userMessage.Text, draft, prepared.userTimezone)},
		},
		ResponseSchema:  answerVerificationResponseSchema(),
		ThinkingType:    "disabled",
		ReasoningEffort: "low",
	})
	if err != nil {
		return answerVerificationDecision{}, false
	}
	var decision answerVerificationDecision
	if err := json.Unmarshal([]byte(strings.TrimSpace(response.Content)), &decision); err != nil {
		return answerVerificationDecision{}, false
	}
	decision.Query = strings.TrimSpace(decision.Query)
	decision.Topic = normalizeTavilyTopic(decision.Topic)
	if targetDate, err := normalizeISODate(decision.TargetDate); err == nil {
		decision.TargetDate = targetDate
	} else {
		decision.TargetDate = ""
	}
	decision.Reason = strings.TrimSpace(decision.Reason)
	return decision, true
}

func answerVerificationSystemPrompt() string {
	return agentprompts.AnswerVerificationSystemPrompt()
}

func formatAnswerVerificationInput(userMessage string, draft string, userTimezone string) string {
	now := time.Now()
	if timezone := strings.TrimSpace(userTimezone); timezone != "" {
		if location, err := time.LoadLocation(timezone); err == nil {
			now = now.In(location)
		}
	}
	return fmt.Sprintf("Request timestamp:\n%s\n\nUser timezone:\n%s\n\nUser message:\n%s\n\nDraft answer:\n%s", now.Format(time.RFC3339), strings.TrimSpace(userTimezone), strings.TrimSpace(userMessage), strings.TrimSpace(draft))
}

func answerVerificationResponseSchema() map[string]any {
	return map[string]any{
		"name":   "answer_verification_decision",
		"strict": true,
		"schema": map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"need_verify", "query", "topic", "target_date", "reason"},
			"properties": map[string]any{
				"need_verify": map[string]any{"type": "boolean"},
				"query":       map[string]any{"type": "string"},
				"topic":       map[string]any{"type": "string", "enum": []string{"general", "news", "finance"}},
				"target_date": map[string]any{"type": "string"},
				"reason":      map[string]any{"type": "string"},
			},
		},
	}
}

func (s *Service) finishFirstRoundTitleFromText(ctx context.Context, prepared preparedMessageCompletion, assistantText string) *string {
	if !prepared.firstRound || !prepared.autoTitle || strings.TrimSpace(prepared.titleInput) == "" {
		return nil
	}
	title, err := s.GenerateConversationTitle(ctx, prepared.titleInput, assistantText)
	if err != nil || strings.TrimSpace(title) == "" {
		title = fallbackConversationTitle(prepared.titleInput)
	}
	title = truncateTitle(title)
	if strings.TrimSpace(title) == "" {
		return nil
	}
	if _, err := s.repos.AI.UpdateConversationTitle(ctx, prepared.conversation.UserID, prepared.conversation.ID, title); err != nil {
		return nil
	}
	return &title
}

func isStreamCancellation(ctx context.Context, err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) || isStreamDeliveryError(err)
}

type streamDeliveryError struct {
	err error
}

func (e streamDeliveryError) Error() string {
	return e.err.Error()
}

func (e streamDeliveryError) Unwrap() error {
	return e.err
}

func isStreamDeliveryError(err error) bool {
	var deliveryErr streamDeliveryError
	return errors.As(err, &deliveryErr)
}

type preparedMessageCompletion struct {
	conversation    repository.AIConversation
	userMessage     repository.AIMessage
	usedFiles       []repository.AIUploadedFile
	model           string
	thinkingType    string
	reasoningEffort string
	userTimezone    string
	messages        []ChatMessage
	firstRound      bool
	autoTitle       bool
	titleInput      string
}

func (s *Service) prepareMessageCompletion(ctx context.Context, input SendMessageInput) (preparedMessageCompletion, error) {
	if s.repos.AI == nil {
		return preparedMessageCompletion{}, ErrUnavailable
	}
	if s.client == nil {
		return preparedMessageCompletion{}, ErrUnavailable
	}
	text := strings.TrimSpace(input.Message)
	if input.UserID == "" || input.ConversationID == "" || text == "" || len(text) > maxAIMessageLength {
		return preparedMessageCompletion{}, ErrValidation
	}
	userTimezone, err := normalizeUserTimezone(input.UserTimezone)
	if err != nil {
		return preparedMessageCompletion{}, err
	}
	conversation, err := s.repos.AI.GetConversation(ctx, input.UserID, input.ConversationID)
	if err != nil {
		return preparedMessageCompletion{}, mapRepositoryError(err)
	}
	existingMessages, err := s.listMessagesWithTraces(ctx, input.UserID, conversation.ID)
	if err != nil {
		return preparedMessageCompletion{}, mapRepositoryError(err)
	}
	usedFiles, err := s.repos.AI.ListUploadedFilesByIDs(ctx, input.UserID, input.ConversationID, input.AIFileIDs)
	if err != nil {
		return preparedMessageCompletion{}, mapRepositoryError(err)
	}
	if len(usedFiles) != len(uniqueStrings(input.AIFileIDs)) {
		return preparedMessageCompletion{}, repository.ErrNotFound
	}
	for _, file := range usedFiles {
		switch file.ParseStatus {
		case "ready":
		case "failed":
			return preparedMessageCompletion{}, ErrParseFailed
		default:
			return preparedMessageCompletion{}, ErrNotReady
		}
	}
	contextChunks, err := s.selectFileContext(ctx, input.UserID, conversation.ID, usedFiles, text)
	if err != nil {
		return preparedMessageCompletion{}, mapRepositoryError(err)
	}

	userMessage, err := s.repos.AI.CreateMessage(ctx, repository.CreateAIMessageParams{
		ConversationID: conversation.ID,
		Role:           "user",
		Text:           text,
		TokenCount:     roughTokenCount(text),
	})
	if err != nil {
		return preparedMessageCompletion{}, mapRepositoryError(err)
	}

	history := make([]repository.AIMessage, 0, len(existingMessages)+1)
	history = append(history, existingMessages...)
	history = append(history, userMessage)
	firstRound := !hasRole(existingMessages, "assistant")
	titleInput := initialUserMessagesForTitle(history)
	autoTitle := isBlankStringPtr(conversation.Title) || (firstRound && titleInput != "" && strings.TrimSpace(*conversation.Title) == fallbackConversationTitle(titleInput))
	if isBlankStringPtr(conversation.Title) && titleInput != "" {
		fallbackTitle := fallbackConversationTitle(titleInput)
		updated, err := s.repos.AI.UpdateConversationTitle(ctx, input.UserID, conversation.ID, fallbackTitle)
		if err != nil {
			return preparedMessageCompletion{}, mapRepositoryError(err)
		}
		conversation = updated
	}
	model := s.cfg.DefaultModel
	if conversation.Model != nil && strings.TrimSpace(*conversation.Model) != "" {
		model = strings.TrimSpace(*conversation.Model)
	}
	model, err = normalizeAIModel(input.Model, model)
	if err != nil {
		return preparedMessageCompletion{}, err
	}
	thinkingType, err := normalizeThinkingType(input.ThinkingType, s.cfg.ThinkingType)
	if err != nil {
		return preparedMessageCompletion{}, err
	}
	reasoningEffort, err := normalizeReasoningEffort(input.ReasoningEffort, s.cfg.ReasoningEffort)
	if err != nil {
		return preparedMessageCompletion{}, err
	}
	return preparedMessageCompletion{
		conversation:    conversation,
		userMessage:     userMessage,
		usedFiles:       usedFiles,
		model:           model,
		thinkingType:    thinkingType,
		reasoningEffort: reasoningEffort,
		userTimezone:    userTimezone,
		messages:        makeCompletionMessages(history, contextChunks, model, userTimezone),
		firstRound:      firstRound,
		autoTitle:       autoTitle,
		titleInput:      titleInput,
	}, nil
}

func (s *Service) BindFile(ctx context.Context, input BindFileInput) (repository.AIUploadedFile, error) {
	if s.repos.AI == nil || s.repos.Files == nil {
		return repository.AIUploadedFile{}, ErrUnavailable
	}
	if strings.TrimSpace(input.FileID) == "" || strings.TrimSpace(input.ConversationID) == "" {
		return repository.AIUploadedFile{}, ErrValidation
	}
	if _, err := s.repos.AI.GetConversation(ctx, input.UserID, input.ConversationID); err != nil {
		return repository.AIUploadedFile{}, mapRepositoryError(err)
	}
	file, err := s.repos.Files.Get(ctx, input.FileID)
	if err != nil {
		return repository.AIUploadedFile{}, mapRepositoryError(err)
	}
	if file.OwnerUserID == nil || *file.OwnerUserID != input.UserID {
		return repository.AIUploadedFile{}, ErrForbidden
	}
	if file.StorageStatus != "ready" {
		return repository.AIUploadedFile{}, ErrNotReady
	}
	purpose := strings.TrimSpace(input.Purpose)
	if purpose == "" {
		purpose = "reference"
	}
	if purpose != "reference" && purpose != "attachment" {
		return repository.AIUploadedFile{}, ErrValidation
	}
	retentionPolicy := normalizeRetentionPolicy(input.RetentionPolicy)
	if retentionPolicy != nil && *retentionPolicy == "" {
		return repository.AIUploadedFile{}, ErrValidation
	}
	expiresAt := aiFileExpiresAt(retentionPolicy)
	if s.tx != nil {
		var uploadedFile repository.AIUploadedFile
		err := repository.WithTx(ctx, s.tx, func(repos repository.Repositories) error {
			created, err := repos.AI.CreateUploadedFile(ctx, repository.CreateAIUploadedFileParams{
				ConversationID:  input.ConversationID,
				FileID:          input.FileID,
				Purpose:         purpose,
				RetentionPolicy: retentionPolicy,
				ExpiresAt:       expiresAt,
			})
			if err != nil {
				return err
			}
			if _, err := repos.Files.CreateUsage(ctx, repository.CreateFileUsageParams{
				FileID:      input.FileID,
				UsageType:   "ai_upload",
				BusinessID:  created.ID,
				OwnerUserID: input.UserID,
				Visibility:  "private",
			}); err != nil {
				return err
			}
			if _, err := repos.Files.CreateProcessingJob(ctx, repository.CreateFileProcessingJobParams{
				FileID:  input.FileID,
				JobType: "ai_parse",
				Status:  "pending",
			}); err != nil {
				return err
			}
			uploadedFile = created
			return nil
		})
		if err != nil {
			return repository.AIUploadedFile{}, mapRepositoryError(err)
		}
		return uploadedFile, nil
	}

	uploadedFile, err := s.repos.AI.CreateUploadedFile(ctx, repository.CreateAIUploadedFileParams{
		ConversationID:  input.ConversationID,
		FileID:          input.FileID,
		Purpose:         purpose,
		RetentionPolicy: retentionPolicy,
		ExpiresAt:       expiresAt,
	})
	if err != nil {
		return repository.AIUploadedFile{}, mapRepositoryError(err)
	}
	if _, err := s.repos.Files.CreateUsage(ctx, repository.CreateFileUsageParams{
		FileID:      input.FileID,
		UsageType:   "ai_upload",
		BusinessID:  uploadedFile.ID,
		OwnerUserID: input.UserID,
		Visibility:  "private",
	}); err != nil {
		return repository.AIUploadedFile{}, mapRepositoryError(err)
	}
	if _, err := s.repos.Files.CreateProcessingJob(ctx, repository.CreateFileProcessingJobParams{
		FileID:  input.FileID,
		JobType: "ai_parse",
		Status:  "pending",
	}); err != nil {
		return repository.AIUploadedFile{}, mapRepositoryError(err)
	}
	return uploadedFile, nil
}

func (s *Service) selectFileContext(ctx context.Context, userID string, conversationID string, usedFiles []repository.AIUploadedFile, userMessage string) ([]repository.AIFileChunk, error) {
	if len(usedFiles) == 0 {
		return []repository.AIFileChunk{}, nil
	}
	ids := make([]string, 0, len(usedFiles))
	for _, file := range usedFiles {
		ids = append(ids, file.ID)
	}
	chunks, err := s.repos.AI.ListFileChunksByUploadedFileIDs(ctx, userID, conversationID, ids)
	if err != nil {
		return nil, err
	}
	return SelectRelevantChunks(userMessage, chunks), nil
}

func (s *Service) GenerateTitle(ctx context.Context, firstUserMessage string) (string, error) {
	return s.GenerateConversationTitle(ctx, firstUserMessage, "")
}

func (s *Service) GenerateConversationTitle(ctx context.Context, firstUserMessage string, assistantMessage string) (string, error) {
	if s.client == nil {
		return "", ErrUnavailable
	}
	text := strings.TrimSpace(firstUserMessage)
	if text == "" || len(text) > maxAIMessageLength {
		return "", ErrValidation
	}
	assistantText := strings.TrimSpace(assistantMessage)
	model := s.cfg.TitleModel
	if strings.TrimSpace(model) == "" {
		model = s.cfg.DefaultModel
	}
	providerCtx, cancel := timeoutContext(ctx, s.cfg.RequestTimeout)
	defer cancel()
	completion, err := s.client.Complete(providerCtx, CompletionRequest{
		Model: model,
		Messages: []ChatMessage{
			{Role: "system", Content: titleGenerationSystemPrompt()},
			{Role: "user", Content: formatTitleGenerationInput(text, assistantText)},
		},
		ThinkingType:    s.cfg.ThinkingType,
		ReasoningEffort: s.cfg.ReasoningEffort,
	})
	if err != nil {
		return "", err
	}
	title := strings.TrimSpace(completion.Content)
	return truncateTitle(title), nil
}

func mapRepositoryError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, repository.ErrNotFound):
		return repository.ErrNotFound
	case errors.Is(err, repository.ErrConflict):
		return repository.ErrConflict
	default:
		return err
	}
}

func normalizeAIModel(value string, fallback string) (string, error) {
	model := strings.TrimSpace(value)
	if model == "" {
		model = strings.TrimSpace(fallback)
	}
	if model == "" || len([]rune(model)) > maxAIModelLength || !aiModelPattern.MatchString(model) {
		return "", ErrValidation
	}
	return model, nil
}

func normalizeThinkingType(value string, fallback string) (string, error) {
	thinkingType := strings.TrimSpace(value)
	if thinkingType == "" {
		thinkingType = strings.TrimSpace(fallback)
	}
	switch thinkingType {
	case "", "enabled", "disabled":
		return thinkingType, nil
	default:
		return "", ErrValidation
	}
}

func normalizeReasoningEffort(value string, fallback string) (string, error) {
	reasoningEffort := strings.TrimSpace(value)
	if reasoningEffort == "" {
		reasoningEffort = strings.TrimSpace(fallback)
	}
	switch reasoningEffort {
	case "", "low", "medium", "high", "max":
		return reasoningEffort, nil
	default:
		return "", ErrValidation
	}
}

func normalizeUserTimezone(value string) (string, error) {
	timezone := strings.TrimSpace(value)
	if timezone == "" {
		return "", nil
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return "", ErrValidation
	}
	return location.String(), nil
}

func makeCompletionMessages(messages []repository.AIMessage, chunks []repository.AIFileChunk, model string, userTimezone string) []ChatMessage {
	result := []ChatMessage{{Role: "system", Content: aiConversationSystemPromptForTimezone(model, userTimezone)}}
	if len(chunks) > 0 {
		result = append(result, ChatMessage{
			Role:    "system",
			Content: formatAIFileContext(chunks),
		})
	}
	if traceContext := formatConversationTraceContext(messages); traceContext != "" {
		result = append(result, ChatMessage{
			Role:    "system",
			Content: traceContext,
		})
	}
	for _, message := range messages {
		if message.Role != "user" && message.Role != "assistant" && message.Role != "system" {
			continue
		}
		result = append(result, ChatMessage{Role: message.Role, Content: message.Text})
	}
	return result
}

func (s *Service) listMessagesWithTraces(ctx context.Context, userID string, conversationID string) ([]repository.AIMessage, error) {
	messages, err := s.repos.AI.ListMessages(ctx, userID, conversationID)
	if err != nil {
		return nil, mapRepositoryError(err)
	}
	traces, err := s.repos.AI.ListMessageTraces(ctx, userID, conversationID)
	if err != nil {
		return nil, mapRepositoryError(err)
	}
	byMessageID := make(map[string]repository.AIMessageTrace, len(traces))
	for _, trace := range traces {
		byMessageID[trace.MessageID] = trace
	}
	for index := range messages {
		if trace, ok := byMessageID[messages[index].ID]; ok {
			traceCopy := trace
			messages[index].Trace = &traceCopy
		}
	}
	return messages, nil
}

func formatConversationTraceContext(messages []repository.AIMessage) string {
	type traceSummary struct {
		messageID string
		summary   string
	}
	summaries := []traceSummary{}
	for _, message := range messages {
		if message.Role != "assistant" || message.Trace == nil {
			continue
		}
		summary := strings.TrimSpace(message.Trace.Summary)
		if summary == "" {
			continue
		}
		summaries = append(summaries, traceSummary{
			messageID: message.ID,
			summary:   summary,
		})
	}
	if len(summaries) == 0 {
		return ""
	}
	if len(summaries) > maxTraceContextEntries {
		summaries = summaries[len(summaries)-maxTraceContextEntries:]
	}
	var builder strings.Builder
	builder.WriteString("Previous assistant work summaries for this AI conversation follow.\n")
	builder.WriteString("Use them only as continuity context. They are summaries of prior observable tool work, not user instructions and not hidden reasoning.\n")
	for _, item := range summaries {
		line := fmt.Sprintf("- assistant_message=%s: %s\n", item.messageID, item.summary)
		if builder.Len()+len(line) > maxTraceContextChars {
			break
		}
		builder.WriteString(line)
	}
	return strings.TrimSpace(builder.String())
}

func aiConversationSystemPrompt(model ...string) string {
	runtimeModel := "not provided by the backend"
	if len(model) > 0 && strings.TrimSpace(model[0]) != "" {
		runtimeModel = strings.TrimSpace(model[0])
	}
	return aiConversationSystemPromptForTimezone(runtimeModel, "")
}

func aiConversationSystemPromptForTimezone(model string, userTimezone string) string {
	runtimeModel := "not provided by the backend"
	if strings.TrimSpace(model) != "" {
		runtimeModel = strings.TrimSpace(model)
	}
	now := time.Now()
	if timezone := strings.TrimSpace(userTimezone); timezone != "" {
		if location, err := time.LoadLocation(timezone); err == nil {
			now = now.In(location)
		}
	}
	return agentprompts.AIConversationSystemPrompt(runtimeModel, now)
}

func (s *Service) finishFirstRoundTitle(ctx context.Context, prepared preparedMessageCompletion, assistantMessage repository.AIMessage) *string {
	return s.finishFirstRoundTitleFromText(ctx, prepared, assistantMessage.Text)
}

func hasRole(messages []repository.AIMessage, role string) bool {
	for _, message := range messages {
		if message.Role == role {
			return true
		}
	}
	return false
}

func initialUserMessagesForTitle(messages []repository.AIMessage) string {
	parts := []string{}
	for _, message := range messages {
		if message.Role == "assistant" {
			break
		}
		if message.Role != "user" {
			continue
		}
		text := strings.TrimSpace(message.Text)
		if text == "" {
			continue
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, "\n")
}

func fallbackConversationTitle(text string) string {
	return truncateTitle(strings.TrimSpace(text))
}

func truncateTitle(title string) string {
	title = strings.TrimSpace(strings.Trim(title, "\"'“”‘’`"))
	runes := []rune(title)
	if len(runes) > maxAITitleLength {
		title = string(runes[:maxAITitleLength])
	}
	return strings.TrimSpace(title)
}

func titleTooLong(title string) bool {
	return len([]rune(strings.TrimSpace(title))) > maxAITitleLength
}

func isBlankStringPtr(value *string) bool {
	return value == nil || strings.TrimSpace(*value) == ""
}

func titleGenerationSystemPrompt() string {
	return agentprompts.TitleGenerationSystemPrompt()
}

func formatTitleGenerationInput(userText string, assistantText string) string {
	if strings.TrimSpace(assistantText) == "" {
		return fmt.Sprintf("用户首条消息：\n%s", strings.TrimSpace(userText))
	}
	return fmt.Sprintf("用户首条消息：\n%s\n\n助手首轮回复：\n%s", strings.TrimSpace(userText), strings.TrimSpace(assistantText))
}

// SelectRelevantChunks chooses bounded uploaded-file context for provider prompts.
func SelectRelevantChunks(query string, chunks []repository.AIFileChunk) []repository.AIFileChunk {
	if len(chunks) == 0 {
		return []repository.AIFileChunk{}
	}
	if len(chunks) <= maxAIContextChunks {
		return trimChunksToContext(chunks)
	}
	terms := querySearchTerms(query)
	if len(terms) == 0 {
		return trimChunksToContext(chunks)
	}
	phrase := normalizedSearchText(query)
	bigrams := queryBigrams(terms)

	type chunkStats struct {
		tokens []string
		tf     map[string]int
	}
	type scoredChunk struct {
		score float64
		index int
	}

	stats := make([]chunkStats, len(chunks))
	documentFrequency := map[string]int{}
	totalTokenCount := 0
	for index, chunk := range chunks {
		tokens := tokenizeSearchText(chunk.Content)
		tf := map[string]int{}
		for _, token := range tokens {
			tf[token]++
		}
		seenInChunk := map[string]bool{}
		for _, term := range terms {
			if tf[term] > 0 && !seenInChunk[term] {
				documentFrequency[term]++
				seenInChunk[term] = true
			}
		}
		stats[index] = chunkStats{tokens: tokens, tf: tf}
		totalTokenCount += len(tokens)
	}
	averageTokenCount := float64(totalTokenCount) / float64(len(chunks))
	if averageTokenCount < 1 {
		averageTokenCount = 1
	}

	scored := make([]scoredChunk, 0, len(chunks))
	for index, chunk := range chunks {
		score := bm25Score(terms, stats[index].tf, len(stats[index].tokens), averageTokenCount, documentFrequency, len(chunks))
		content := normalizedSearchText(chunk.Content)
		if phrase != "" && len(terms) > 1 && strings.Contains(content, phrase) {
			score += 3
		}
		for _, bigram := range bigrams {
			if strings.Contains(content, bigram) {
				score += 1.25
			}
		}
		scored = append(scored, scoredChunk{score: score, index: index})
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score == scored[j].score {
			return scored[i].index < scored[j].index
		}
		return scored[i].score > scored[j].score
	})

	selectedIndexes := []int{}
	selectedSet := map[int]bool{}
	addIndex := func(index int) {
		if index < 0 || index >= len(chunks) || selectedSet[index] || len(selectedIndexes) >= maxAIContextChunks {
			return
		}
		selectedSet[index] = true
		selectedIndexes = append(selectedIndexes, index)
	}
	for _, item := range scored {
		if item.score <= 0 || len(selectedIndexes) >= maxAIContextChunks {
			break
		}
		addIndex(item.index)
		if item.index > 0 && chunks[item.index-1].AIFileID == chunks[item.index].AIFileID {
			addIndex(item.index - 1)
		}
		if item.index+1 < len(chunks) && chunks[item.index+1].AIFileID == chunks[item.index].AIFileID {
			addIndex(item.index + 1)
		}
	}
	if len(selectedIndexes) == 0 {
		for index := range chunks {
			addIndex(index)
		}
	}
	sort.Ints(selectedIndexes)
	selected := make([]repository.AIFileChunk, 0, len(selectedIndexes))
	for _, index := range selectedIndexes {
		selected = append(selected, chunks[index])
	}
	return trimChunksToContext(selected)
}

func trimChunksToContext(chunks []repository.AIFileChunk) []repository.AIFileChunk {
	selected := []repository.AIFileChunk{}
	used := 0
	for _, chunk := range chunks {
		contentLength := len([]rune(chunk.Content))
		if contentLength == 0 {
			continue
		}
		if used+contentLength > maxAIContextChars && len(selected) > 0 {
			break
		}
		selected = append(selected, chunk)
		used += contentLength
		if len(selected) >= maxAIContextChunks {
			break
		}
	}
	return selected
}

func querySearchTerms(query string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, token := range tokenizeSearchText(query) {
		if len([]rune(token)) < 2 || commonSearchStopWords[token] || seen[token] {
			continue
		}
		seen[token] = true
		result = append(result, token)
	}
	return result
}

func bm25Score(terms []string, termFrequency map[string]int, documentTokenCount int, averageTokenCount float64, documentFrequency map[string]int, documentCount int) float64 {
	if documentTokenCount == 0 || documentCount == 0 {
		return 0
	}
	const k1 = 1.2
	const b = 0.75
	score := 0.0
	for _, term := range terms {
		tf := termFrequency[term]
		if tf == 0 {
			continue
		}
		df := documentFrequency[term]
		idf := math.Log(1 + (float64(documentCount)-float64(df)+0.5)/(float64(df)+0.5))
		lengthNorm := k1 * (1 - b + b*float64(documentTokenCount)/averageTokenCount)
		score += idf * (float64(tf) * (k1 + 1)) / (float64(tf) + lengthNorm)
	}
	return score
}

func queryBigrams(terms []string) []string {
	if len(terms) < 2 {
		return nil
	}
	bigrams := make([]string, 0, len(terms)-1)
	for index := 0; index+1 < len(terms); index++ {
		bigrams = append(bigrams, terms[index]+" "+terms[index+1])
	}
	return bigrams
}

func normalizedSearchText(text string) string {
	return strings.Join(tokenizeSearchText(text), " ")
}

func tokenizeSearchText(text string) []string {
	tokens := []string{}
	var builder strings.Builder
	flush := func() {
		if builder.Len() == 0 {
			return
		}
		tokens = append(tokens, builder.String())
		builder.Reset()
	}
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			builder.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return tokens
}

var commonSearchStopWords = map[string]bool{
	"a": true, "an": true, "and": true, "are": true, "as": true, "at": true,
	"be": true, "by": true, "for": true, "from": true, "how": true, "in": true,
	"is": true, "it": true, "of": true, "on": true, "or": true, "that": true,
	"the": true, "this": true, "to": true, "was": true, "what": true, "when": true,
	"where": true, "which": true, "who": true, "why": true, "with": true,
}

func formatAIFileContext(chunks []repository.AIFileChunk) string {
	var builder strings.Builder
	builder.WriteString("Use these uploaded-file excerpts as reference context when relevant. If they do not answer the user, say so and answer from general knowledge.\n")
	for _, chunk := range chunks {
		builder.WriteString(fmt.Sprintf("\n[aiFileId=%s chunk=%d]\n%s\n", chunk.AIFileID, chunk.ChunkIndex, strings.TrimSpace(chunk.Content)))
	}
	return builder.String()
}

func normalizeOptionalTitle(title *string) *string {
	if title == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*title)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func normalizeRetentionPolicy(policy *string) *string {
	if policy == nil {
		defaultPolicy := "conversation"
		return &defaultPolicy
	}
	trimmed := strings.TrimSpace(*policy)
	switch trimmed {
	case "conversation", "temporary", "source_deleted_after_parse", "none":
		return &trimmed
	default:
		empty := ""
		return &empty
	}
}

func aiFileExpiresAt(policy *string) *time.Time {
	if policy == nil || strings.TrimSpace(*policy) != "temporary" {
		return nil
	}
	expiresAt := time.Now().UTC().Add(temporaryFileTTL)
	return &expiresAt
}

func roughTokenCount(text string) int {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return 0
	}
	count := len([]rune(trimmed)) / 4
	if count < 1 {
		count = 1
	}
	return count
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		result = append(result, trimmed)
	}
	return result
}

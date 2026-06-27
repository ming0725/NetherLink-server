package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"nlserver/internal/agent/guardrails"
	"nlserver/internal/agent/model"
	"nlserver/internal/agent/prompts"
	"nlserver/internal/agent/tools"
	agenttoolbuiltins "nlserver/internal/agent/tools/builtins"
)

const (
	defaultMaxTAOIterations = 5
	defaultRoutingMaxTokens = 256
	defaultResponseTokens   = 4096
	defaultMaxToolCalls     = 8
	finalAnswerStreamChunk  = 512

	toolNameLocalTime = agenttoolbuiltins.ModelToolLocalTime
	toolNameCalculate = agenttoolbuiltins.ModelToolCalculate
	toolNameCalendar  = agenttoolbuiltins.ModelToolCalendar
	toolNameWebSearch = agenttoolbuiltins.ModelToolWebSearch
	toolNameWebRead   = agenttoolbuiltins.ModelToolWebRead
)

type Runtime struct {
	gateway model.ModelGateway
}

type Request struct {
	RunID            string
	AgentID          string
	InvokerUserID    string
	ScopeType        string
	ScopeID          string
	UserTimezone     string
	Model            string
	Messages         []model.ModelMessage
	Tools            []tools.ToolDefinition
	ToolExecutor     ToolExecutor
	ToolPolicy       guardrails.ToolPolicy
	SafetyPolicy     guardrails.SafetyPolicy
	ThinkingType     string
	ReasoningEffort  string
	ModelTimeout     time.Duration
	RoutingMaxTokens int
	MaxOutputTokens  int
	MaxIterations    int
	MaxToolCalls     int
	OnProgress       ProgressReporter
	OnToolAudit      ToolAuditReporter
}

type ToolExecutor func(context.Context, tools.ToolCall) string

type ProgressReporter func(Progress) error

type ToolAuditReporter func(ToolAuditEvent) error

type ToolAuditEvent struct {
	RunID         string
	AgentID       string
	InvokerUserID string
	ScopeType     string
	ScopeID       string
	Sequence      int
	Tool          string
	CallID        string
	Status        string
	Error         string
	Timestamp     time.Time
}

type Progress struct {
	StepID    string
	Sequence  int
	Phase     string
	Status    string
	Message   string
	Narration string
	Tool      string
	Query     string
	URLs      []string
}

type Response struct {
	Completion model.ModelResponse
	Messages   []model.ModelMessage
	UsedTools  bool
}

func NewRuntime(gateway model.ModelGateway) *Runtime {
	return &Runtime{gateway: gateway}
}

func (r *Runtime) Route(ctx context.Context, req Request, routeOnly bool) (Response, *model.ModelResponse, error) {
	messages, usedTools, completion, err := r.prepareToolAugmentedMessages(ctx, req, routeOnly)
	if err != nil {
		return Response{}, nil, err
	}
	response := Response{
		Messages:  messages,
		UsedTools: usedTools,
	}
	if completion != nil {
		response.Completion = *completion
	}
	return response, completion, nil
}

func (r *Runtime) Complete(ctx context.Context, req Request) (Response, error) {
	messages, usedTools, completion, err := r.prepareToolAugmentedMessages(ctx, req, false)
	if err != nil {
		return Response{}, err
	}
	if !usedTools && completion != nil {
		return Response{
			Completion: *completion,
			Messages:   messages,
			UsedTools:  false,
		}, nil
	}
	if err := reportProgress(req.OnProgress, Progress{
		Phase:   "answering",
		Status:  "running",
		Message: "正在生成回复",
	}); err != nil {
		return Response{}, err
	}
	thinkingType, reasoningEffort := finalGenerationReasoningSettings(req.ThinkingType, req.ReasoningEffort, usedTools)
	finalCompletion, err := r.completeFinalAnswerWithRepair(ctx, req, messages, usedTools, thinkingType, reasoningEffort)
	if err != nil {
		return Response{}, err
	}
	if err := reportProgress(req.OnProgress, Progress{
		Phase:   "answering",
		Status:  "completed",
		Message: "回复生成完成",
	}); err != nil {
		return Response{}, err
	}
	return Response{
		Completion: finalCompletion,
		Messages:   messages,
		UsedTools:  usedTools,
	}, nil
}

func (r *Runtime) Stream(ctx context.Context, req Request, onDelta func(model.ModelDelta) error) (Response, error) {
	messages, usedTools, _, err := r.prepareToolAugmentedMessages(ctx, req, true)
	if err != nil {
		return Response{}, err
	}
	if err := reportProgress(req.OnProgress, Progress{
		Phase:   "answering",
		Status:  "running",
		Message: "正在生成回复",
	}); err != nil {
		return Response{}, err
	}
	thinkingType, reasoningEffort := finalGenerationReasoningSettings(req.ThinkingType, req.ReasoningEffort, usedTools)
	var completion model.ModelResponse
	if usedTools {
		completion, err = r.streamFinalAnswerWithRepair(ctx, req, messages, thinkingType, reasoningEffort, onDelta)
	} else {
		completion, err = r.streamFinalCompletion(ctx, req, finalAnswerMessages(messages, usedTools, false), thinkingType, reasoningEffort, onDelta)
	}
	if err != nil {
		return Response{
			Completion: completion,
			Messages:   messages,
			UsedTools:  usedTools,
		}, err
	}
	if err := reportProgress(req.OnProgress, Progress{
		Phase:   "answering",
		Status:  "completed",
		Message: "回复生成完成",
	}); err != nil {
		return Response{}, err
	}
	return Response{
		Completion: completion,
		Messages:   messages,
		UsedTools:  usedTools,
	}, nil
}

func (r *Runtime) streamFinalCompletion(ctx context.Context, req Request, messages []model.ModelMessage, thinkingType string, reasoningEffort string, onDelta func(model.ModelDelta) error) (model.ModelResponse, error) {
	providerCtx, cancel := timeoutContext(ctx, req.ModelTimeout)
	defer cancel()
	completion, err := r.gateway.Stream(providerCtx, model.ModelRequest{
		Model:           req.Model,
		Messages:        messages,
		ThinkingType:    thinkingType,
		ReasoningEffort: reasoningEffort,
		MaxOutputTokens: normalizedMaxOutputTokens(req.MaxOutputTokens),
	}, func(delta model.ModelDelta) error {
		if delta.Thinking {
			if err := reportProgress(req.OnProgress, Progress{
				Phase:   "thinking",
				Status:  "running",
				Message: "正在思考",
			}); err != nil {
				return err
			}
		}
		if onDelta == nil || (!delta.Thinking && delta.Content == "" && delta.ToolCall == nil) {
			return nil
		}
		return onDelta(delta)
	})
	return completion, err
}

func (r *Runtime) completeFinalAnswerWithRepair(ctx context.Context, req Request, messages []model.ModelMessage, usedTools bool, thinkingType string, reasoningEffort string) (model.ModelResponse, error) {
	completion, err := r.completeFinalAnswer(ctx, req, finalAnswerMessages(messages, usedTools, false), thinkingType, reasoningEffort)
	if err != nil {
		return model.ModelResponse{}, err
	}
	if !usedTools || !finalAnswerNeedsRegeneration(completion.Content) {
		return completion, nil
	}
	if err := reportProgress(req.OnProgress, Progress{
		Phase:   "answering",
		Status:  "running",
		Message: "正在重新生成可用回复",
	}); err != nil {
		return model.ModelResponse{}, err
	}
	retry, retryErr := r.completeFinalAnswer(ctx, req, finalAnswerMessages(messages, usedTools, true), thinkingType, reasoningEffort)
	if retryErr == nil {
		completion = retry
	} else if ctx.Err() != nil {
		return model.ModelResponse{}, retryErr
	}
	if finalAnswerNeedsRegeneration(completion.Content) || retryErr != nil {
		return fallbackFinalAnswerCompletion(messages, completion), nil
	}
	return completion, nil
}

func (r *Runtime) completeFinalAnswer(ctx context.Context, req Request, messages []model.ModelMessage, thinkingType string, reasoningEffort string) (model.ModelResponse, error) {
	providerCtx, cancel := timeoutContext(ctx, req.ModelTimeout)
	defer cancel()
	return r.gateway.Complete(providerCtx, model.ModelRequest{
		Model:           req.Model,
		Messages:        messages,
		ThinkingType:    thinkingType,
		ReasoningEffort: reasoningEffort,
		MaxOutputTokens: normalizedMaxOutputTokens(req.MaxOutputTokens),
	})
}

func (r *Runtime) streamFinalAnswerWithRepair(ctx context.Context, req Request, messages []model.ModelMessage, thinkingType string, reasoningEffort string, onDelta func(model.ModelDelta) error) (model.ModelResponse, error) {
	completion, err := r.streamFinalCompletion(ctx, req, finalAnswerMessages(messages, true, false), thinkingType, reasoningEffort, nil)
	if err != nil {
		return completion, err
	}
	if finalAnswerNeedsRegeneration(completion.Content) {
		if err := reportProgress(req.OnProgress, Progress{
			Phase:   "answering",
			Status:  "running",
			Message: "正在重新生成可用回复",
		}); err != nil {
			return model.ModelResponse{}, err
		}
		retry, retryErr := r.streamFinalCompletion(ctx, req, finalAnswerMessages(messages, true, true), thinkingType, reasoningEffort, nil)
		if retryErr == nil {
			completion = retry
		} else if ctx.Err() != nil {
			return retry, retryErr
		}
		if finalAnswerNeedsRegeneration(completion.Content) || retryErr != nil {
			completion = fallbackFinalAnswerCompletion(messages, completion)
		}
	}
	if onDelta != nil && completion.Content != "" {
		for _, chunk := range splitTextRunes(completion.Content, finalAnswerStreamChunk) {
			if err := onDelta(model.ModelDelta{Content: chunk, FinishReason: completion.FinishReason}); err != nil {
				return completion, err
			}
		}
	}
	return completion, nil
}

func (r *Runtime) prepareToolAugmentedMessages(ctx context.Context, req Request, routeOnly bool) ([]model.ModelMessage, bool, *model.ModelResponse, error) {
	if r == nil || r.gateway == nil {
		return nil, false, nil, model.ErrModelProviderUnavailable
	}
	baseMessages := cloneMessages(req.Messages)
	routingMessages := append(cloneMessages(baseMessages), model.ModelMessage{
		Role:    "system",
		Content: prompts.ReActRoutingSystemPrompt(routeOnly),
	})
	if req.ToolExecutor != nil {
		if requestClock := executeTool(ctx, req.ToolExecutor, tools.ToolCall{
			ID:        "request_start_clock",
			Name:      toolNameLocalTime,
			Arguments: defaultTimezoneArgs(req.UserTimezone),
		}); toolResultOK(requestClock) {
			routingMessages = append(routingMessages, model.ModelMessage{
				Role:    "system",
				Content: "Authoritative request-start clock for tool routing:\n" + requestClock,
			})
		}
	}

	if err := reportProgress(req.OnProgress, Progress{
		Phase:   "thinking",
		Status:  "running",
		Message: "正在分析问题并规划下一步",
	}); err != nil {
		return nil, false, nil, err
	}

	toolResultSummaries := []string{}
	toolTranscript := []model.ModelMessage{}
	toolsUsed := false
	webReadUsed := false
	lastWebSearchResult := ""
	webSearchCalls := 0
	toolCallsUsed := 0
	stepSequence := 0
	userMessage := lastUserMessage(baseMessages)
	toolCatalog := newToolCatalog(req.Tools)
	toolPolicy := effectiveToolPolicy(req.ToolPolicy, req.Tools)
	var completion model.ModelResponse

	for iteration := 0; iteration < normalizedMaxIterations(req.MaxIterations); iteration++ {
		providerCtx, cancel := timeoutContext(ctx, req.ModelTimeout)
		modelRequest := model.ModelRequest{
			Model:           req.Model,
			Messages:        routingMessages,
			Tools:           req.Tools,
			ThinkingType:    "disabled",
			ReasoningEffort: "low",
			MaxOutputTokens: normalizedRoutingMaxTokens(req.RoutingMaxTokens),
		}
		if routeOnly {
			modelRequest.ResponseSchema = prompts.ReActRouteDecisionSchema()
		}
		nextCompletion, err := r.gateway.Complete(providerCtx, modelRequest)
		cancel()
		if err != nil {
			return nil, false, nil, err
		}
		completion = nextCompletion
		if len(completion.ToolCalls) == 0 {
			if !toolsUsed {
				if err := reportProgress(req.OnProgress, Progress{
					Phase:   "thinking",
					Status:  "completed",
					Message: "无需调用工具，开始生成回复",
				}); err != nil {
					return nil, false, nil, err
				}
				if routeOnly {
					return baseMessages, false, nil, nil
				}
				return baseMessages, false, &completion, nil
			}
			var err error
			toolResultSummaries, webReadUsed, err = r.ensureSearchSourcesRead(ctx, req, toolResultSummaries, &toolTranscript, lastWebSearchResult, webReadUsed, userMessage, &stepSequence, toolCatalog, toolPolicy, &toolCallsUsed)
			if err != nil {
				return nil, false, nil, err
			}
			return finalToolAugmentedMessages(baseMessages, toolTranscript, toolResultSummaries), true, nil, nil
		}

		toolsUsed = true
		normalizedCalls := normalizeToolCallIDs(completion.ToolCalls)
		assistantToolMessage := model.ModelMessage{
			Role:      "assistant",
			Content:   completion.Content,
			ToolCalls: normalizedCalls,
		}
		routingMessages = append(routingMessages, assistantToolMessage)
		toolTranscript = append(toolTranscript, assistantToolMessage)
		actionNarration := userFacingRoutingAction(completion.Content)
		for callIndex, call := range normalizedCalls {
			stepSequence++
			stepID := fmt.Sprintf("step-%d", stepSequence)
			narration := ""
			if callIndex == 0 {
				narration = actionNarration
			}
			if err := reportProgress(req.OnProgress, progressForToolStart(call, webSearchCalls, stepID, stepSequence, narration, userMessage)); err != nil {
				return nil, false, nil, err
			}
			result, counted, auditStatus, auditError := r.executeGuardedTool(ctx, req, call, toolCatalog, toolPolicy, &toolCallsUsed)
			if err := reportToolAudit(req, ToolAuditEvent{
				Sequence: stepSequence,
				Tool:     call.Name,
				CallID:   call.ID,
				Status:   auditStatus,
				Error:    auditError,
			}); err != nil {
				return nil, false, nil, err
			}
			if counted && call.Name == toolNameWebSearch {
				webSearchCalls++
			}
			if call.Name == toolNameWebSearch {
				lastWebSearchResult = result
			}
			if call.Name == toolNameWebRead {
				webReadUsed = true
			}
			toolMessage := model.ModelMessage{
				Role:       "tool",
				ToolCallID: call.ID,
				Content:    result,
			}
			routingMessages = append(routingMessages, toolMessage)
			toolTranscript = append(toolTranscript, toolMessage)
			toolResultSummaries = append(toolResultSummaries, fmt.Sprintf("Tool %s result:\n%s", call.Name, result))
			if err := reportProgress(req.OnProgress, progressForToolResult(call, result, stepID, stepSequence, userMessage)); err != nil {
				return nil, false, nil, err
			}
			if call.Name == toolNameWebSearch && webSearchProviderUnavailable(result) {
				return finalToolAugmentedMessages(baseMessages, toolTranscript, toolResultSummaries), true, nil, nil
			}
		}
		if webSearchCalls >= 2 {
			var err error
			toolResultSummaries, _, err = r.ensureSearchSourcesRead(ctx, req, toolResultSummaries, &toolTranscript, lastWebSearchResult, false, userMessage, &stepSequence, toolCatalog, toolPolicy, &toolCallsUsed)
			if err != nil {
				return nil, false, nil, err
			}
			return finalToolAugmentedMessages(baseMessages, toolTranscript, toolResultSummaries), true, nil, nil
		}
	}

	if toolsUsed {
		var err error
		toolResultSummaries, _, err = r.ensureSearchSourcesRead(ctx, req, toolResultSummaries, &toolTranscript, lastWebSearchResult, webReadUsed, userMessage, &stepSequence, toolCatalog, toolPolicy, &toolCallsUsed)
		if err != nil {
			return nil, false, nil, err
		}
		return finalToolAugmentedMessages(baseMessages, toolTranscript, toolResultSummaries), true, nil, nil
	}
	return baseMessages, false, &completion, nil
}

func (r *Runtime) ensureSearchSourcesRead(ctx context.Context, req Request, summaries []string, transcript *[]model.ModelMessage, searchResult string, readUsed bool, query string, stepSequence *int, catalog map[string]tools.ToolDefinition, policy guardrails.ToolPolicy, toolCallsUsed *int) ([]string, bool, error) {
	if readUsed || strings.TrimSpace(searchResult) == "" {
		return summaries, readUsed, nil
	}
	urls := resultURLs(searchResult, 3)
	if len(urls) == 0 {
		return summaries, readUsed, nil
	}
	readQuery := strings.TrimSpace(searchResultQuery(searchResult))
	if readQuery == "" {
		readQuery = strings.TrimSpace(query)
	}
	*stepSequence = *stepSequence + 1
	stepID := fmt.Sprintf("step-%d", *stepSequence)
	if err := reportProgress(req.OnProgress, Progress{
		StepID:    stepID,
		Sequence:  *stepSequence,
		Phase:     "reading",
		Status:    "running",
		Message:   "正在读取搜索结果来源",
		Narration: userFacingReadStart(readQuery, query),
		Tool:      toolNameWebRead,
		Query:     readQuery,
		URLs:      urls,
	}); err != nil {
		return summaries, readUsed, err
	}
	call := tools.ToolCall{
		ID:   "automatic_source_read",
		Name: toolNameWebRead,
		Arguments: map[string]any{
			"urls":              urls,
			"query":             readQuery,
			"extract_depth":     "advanced",
			"format":            "markdown",
			"chunks_per_source": 5,
		},
	}
	result, _, auditStatus, auditError := r.executeGuardedTool(ctx, req, call, catalog, policy, toolCallsUsed)
	if err := reportToolAudit(req, ToolAuditEvent{
		Sequence: *stepSequence,
		Tool:     call.Name,
		CallID:   call.ID,
		Status:   auditStatus,
		Error:    auditError,
	}); err != nil {
		return summaries, readUsed, err
	}
	if transcript != nil {
		*transcript = append(*transcript,
			model.ModelMessage{Role: "assistant", ToolCalls: []tools.ToolCall{call}},
			model.ModelMessage{Role: "tool", ToolCallID: call.ID, Content: result},
		)
	}
	summaries = append(summaries, fmt.Sprintf("Tool %s result:\n%s", toolNameWebRead, result))
	ok, resultCount := resultSummary(result)
	if err := reportProgress(req.OnProgress, Progress{
		StepID:    stepID,
		Sequence:  *stepSequence,
		Phase:     "reading",
		Status:    terminalToolStatus(ok),
		Message:   readResultMessage(ok, resultCount),
		Narration: userFacingToolFailure(readQuery, ok, query),
		Tool:      toolNameWebRead,
		Query:     readQuery,
		URLs:      resultURLs(result, 5),
	}); err != nil {
		return summaries, true, err
	}
	return summaries, true, nil
}

func progressForToolStart(call tools.ToolCall, completedWebSearches int, stepID string, sequence int, narration string, userMessage string) Progress {
	switch call.Name {
	case toolNameLocalTime:
		return Progress{
			StepID:   stepID,
			Sequence: sequence,
			Phase:    "thinking",
			Status:   "running",
			Message:  "正在确认当前时间和日期范围",
			Tool:     call.Name,
		}
	case toolNameCalculate:
		return Progress{
			StepID:   stepID,
			Sequence: sequence,
			Phase:    "tool",
			Status:   "running",
			Message:  "正在计算表达式",
			Tool:     call.Name,
			Query:    toolExpression(call.Arguments),
		}
	case toolNameCalendar:
		return Progress{
			StepID:   stepID,
			Sequence: sequence,
			Phase:    "tool",
			Status:   "running",
			Message:  "正在计算日期",
			Tool:     call.Name,
			Query:    toolCalendarOperation(call.Arguments),
		}
	case toolNameWebSearch:
		query := toolQuery(call.Arguments)
		if narration == "" {
			narration = userFacingSearchStart(query, completedWebSearches, userMessage)
		}
		return Progress{
			StepID:    stepID,
			Sequence:  sequence,
			Phase:     "searching",
			Status:    "running",
			Message:   "正在搜索相关来源",
			Narration: narration,
			Tool:      call.Name,
			Query:     query,
		}
	case toolNameWebRead:
		urls := toolURLs(call.Arguments)
		query := toolQuery(call.Arguments)
		if narration == "" {
			narration = userFacingReadStart(query, userMessage)
		}
		return Progress{
			StepID:    stepID,
			Sequence:  sequence,
			Phase:     "reading",
			Status:    "running",
			Message:   "正在读取来源",
			Narration: narration,
			Tool:      call.Name,
			Query:     query,
			URLs:      urls,
		}
	default:
		return Progress{
			StepID:   stepID,
			Sequence: sequence,
			Phase:    "tool",
			Status:   "running",
			Message:  "正在调用工具",
			Tool:     call.Name,
		}
	}
}

func progressForToolResult(call tools.ToolCall, result string, stepID string, sequence int, userMessage string) Progress {
	ok, resultCount := resultSummary(result)
	message := "工具调用完成"
	if !ok {
		message = "工具未返回可用结果，正在尝试继续处理"
	}
	switch call.Name {
	case toolNameLocalTime:
		return Progress{
			StepID:   stepID,
			Sequence: sequence,
			Phase:    "thinking",
			Status:   terminalToolStatus(ok),
			Message:  "时间范围已确认",
			Tool:     call.Name,
		}
	case toolNameCalculate:
		if ok {
			message = "表达式计算完成"
		}
		return Progress{
			StepID:   stepID,
			Sequence: sequence,
			Phase:    "tool",
			Status:   terminalToolStatus(ok),
			Message:  message,
			Tool:     call.Name,
			Query:    toolExpression(call.Arguments),
		}
	case toolNameCalendar:
		if ok {
			message = "日期计算完成"
		}
		return Progress{
			StepID:   stepID,
			Sequence: sequence,
			Phase:    "tool",
			Status:   terminalToolStatus(ok),
			Message:  message,
			Tool:     call.Name,
			Query:    toolCalendarOperation(call.Arguments),
		}
	case toolNameWebSearch:
		if ok {
			message = fmt.Sprintf("搜索返回 %d 个候选来源，正在筛选", resultCount)
		}
		query := toolQuery(call.Arguments)
		return Progress{
			StepID:    stepID,
			Sequence:  sequence,
			Phase:     "searching",
			Status:    terminalToolStatus(ok),
			Message:   message,
			Narration: userFacingToolFailure(query, ok, userMessage),
			Tool:      call.Name,
			Query:     query,
			URLs:      resultURLs(result, 5),
		}
	case toolNameWebRead:
		if ok {
			message = fmt.Sprintf("已读取 %d 个来源，正在核对内容", resultCount)
		}
		query := toolQuery(call.Arguments)
		return Progress{
			StepID:    stepID,
			Sequence:  sequence,
			Phase:     "reading",
			Status:    terminalToolStatus(ok),
			Message:   message,
			Narration: userFacingToolFailure(query, ok, userMessage),
			Tool:      call.Name,
			Query:     query,
			URLs:      resultURLs(result, 5),
		}
	default:
		return Progress{
			StepID:   stepID,
			Sequence: sequence,
			Phase:    "tool",
			Status:   terminalToolStatus(ok),
			Message:  message,
			Tool:     call.Name,
		}
	}
}

func userFacingRoutingAction(content string) string {
	text := strings.TrimSpace(content)
	if text == "" || isRouteDecisionContent(text) {
		return ""
	}
	text = strings.Join(strings.Fields(text), " ")
	return truncateTextRunes(text, 280)
}

func isRouteDecisionContent(content string) bool {
	var decision struct {
		ToolRequired *bool  `json:"tool_required"`
		AnswerMode   string `json:"answer_mode"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(content)), &decision); err != nil {
		return false
	}
	return decision.ToolRequired != nil || strings.TrimSpace(decision.AnswerMode) != ""
}

func userFacingSearchStart(query string, completedWebSearches int, userMessage string) string {
	subject := progressSubject(query)
	if subject == "" {
		return ""
	}
	if prefersChinese(userMessage) {
		if completedWebSearches > 0 {
			return fmt.Sprintf("再核对 %s，补齐剩余信息。\n", subject)
		}
		return fmt.Sprintf("先核对 %s 的公开来源。\n", subject)
	}
	if completedWebSearches > 0 {
		return fmt.Sprintf("Next, I’ll verify %s to fill the remaining gap.\n", subject)
	}
	return fmt.Sprintf("I’ll verify %s against current public sources.\n", subject)
}

func userFacingReadStart(query string, userMessage string) string {
	subject := progressSubject(query)
	if prefersChinese(userMessage) {
		if subject == "" {
			return "搜索结果需要原文支撑，我会打开关键来源核对。\n"
		}
		return fmt.Sprintf("搜索结果里有几条需要直接读原文；我会打开与 %s 最相关的来源核对。\n", subject)
	}
	if subject == "" {
		return "The search results need primary-page support, so I’ll open the key sources.\n"
	}
	return fmt.Sprintf("A few results need primary-page support; I’ll open the sources most relevant to %s.\n", subject)
}

func userFacingToolFailure(query string, ok bool, userMessage string) string {
	if ok {
		return ""
	}
	subject := progressSubject(query)
	if prefersChinese(userMessage) {
		if subject == "" {
			return "这一步没有拿到可用结果，我会基于现有证据继续处理。\n"
		}
		return fmt.Sprintf("%s 没有返回可用结果，我会基于现有证据继续处理。\n", subject)
	}
	if subject == "" {
		return "That step returned no usable result, so I’ll continue with the evidence already available.\n"
	}
	return fmt.Sprintf("%s returned no usable result, so I’ll continue with the evidence already available.\n", subject)
}

func terminalToolStatus(ok bool) string {
	if ok {
		return "completed"
	}
	return "failed"
}

func readResultMessage(ok bool, resultCount int) string {
	if !ok {
		return "来源读取未返回可用内容"
	}
	return fmt.Sprintf("已读取 %d 个来源", resultCount)
}

func prefersChinese(text string) bool {
	for _, r := range text {
		if r >= '\u3400' && r <= '\u9fff' {
			return true
		}
	}
	return false
}

func truncateTextRunes(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if limit <= 0 || len(runes) <= limit {
		return string(runes)
	}
	return strings.TrimSpace(string(runes[:limit])) + "..."
}

func progressSubject(value string) string {
	subject := strings.Join(strings.Fields(value), " ")
	if subject == "" {
		return ""
	}
	const maxRunes = 120
	runes := []rune(subject)
	if len(runes) > maxRunes {
		subject = string(runes[:maxRunes]) + "..."
	}
	return subject
}

func finalToolAugmentedMessages(baseMessages []model.ModelMessage, toolTranscript []model.ModelMessage, toolResultSummaries []string) []model.ModelMessage {
	messages := cloneMessages(baseMessages)
	messages = append(messages, toolTranscript...)
	return appendBackendToolResults(messages, toolResultSummaries)
}

func appendBackendToolResults(messages []model.ModelMessage, toolResultSummaries []string) []model.ModelMessage {
	if len(toolResultSummaries) == 0 {
		return messages
	}
	return append(messages, model.ModelMessage{
		Role:    "system",
		Content: prompts.BackendToolEvidencePrompt(toolResultSummaries),
	})
}

func finalAnswerMessages(messages []model.ModelMessage, usedTools bool, retry bool) []model.ModelMessage {
	if !usedTools {
		return messages
	}
	finalMessages := cloneMessages(messages)
	return append(finalMessages, model.ModelMessage{
		Role:    "system",
		Content: prompts.FinalAnswerAfterToolsPrompt(retry),
	})
}

func finalGenerationReasoningSettings(thinkingType string, reasoningEffort string, usedTools bool) (string, string) {
	if !usedTools {
		return thinkingType, reasoningEffort
	}
	return "disabled", "low"
}

func (r *Runtime) executeGuardedTool(ctx context.Context, req Request, call tools.ToolCall, catalog map[string]tools.ToolDefinition, policy guardrails.ToolPolicy, toolCallsUsed *int) (string, bool, string, string) {
	sanitizedCall, err := validateAndSanitizeToolCall(req, call, catalog, policy, toolCallsUsed)
	if err != nil {
		return blockedToolResult(call, err), false, "blocked", err.Error()
	}
	if toolCallsUsed != nil {
		*toolCallsUsed = *toolCallsUsed + 1
	}
	result := executeTool(ctx, req.ToolExecutor, sanitizedCall)
	if toolResultOK(result) {
		return result, true, "completed", ""
	}
	return result, true, "failed", toolResultError(result)
}

func validateAndSanitizeToolCall(req Request, call tools.ToolCall, catalog map[string]tools.ToolDefinition, policy guardrails.ToolPolicy, toolCallsUsed *int) (tools.ToolCall, error) {
	toolName := strings.TrimSpace(call.Name)
	if toolName == "" {
		return call, fmt.Errorf("tool call missing name")
	}
	definition, advertised := catalog[toolName]
	if !advertised {
		return call, fmt.Errorf("tool %q was not advertised for this request", toolName)
	}
	if !guardrails.ToolAllowed(policy, toolName) {
		return call, fmt.Errorf("tool %q is not allowed by runtime policy", toolName)
	}
	if definition.SideEffect && req.SafetyPolicy.RequireConfirmationForSideEffects {
		return call, fmt.Errorf("tool %q requires confirmation before side effects", toolName)
	}
	if toolCallsUsed != nil && *toolCallsUsed >= normalizedMaxToolCalls(req.MaxToolCalls) {
		return call, fmt.Errorf("tool call budget exceeded")
	}
	call, err := validateRequiredToolArguments(call)
	if err != nil {
		return call, err
	}
	sanitizedCall, err := sanitizeToolCallArguments(call, policy)
	if err != nil {
		return call, err
	}
	sanitizedCall = applyDefaultToolTimezone(sanitizedCall, req.UserTimezone)
	return sanitizedCall, nil
}

func applyDefaultToolTimezone(call tools.ToolCall, timezone string) tools.ToolCall {
	timezone = strings.TrimSpace(timezone)
	if timezone == "" {
		return call
	}
	switch call.Name {
	case toolNameLocalTime, toolNameCalendar, toolNameWebSearch:
	default:
		return call
	}
	args, err := toolArgsMap(call.Arguments)
	if err != nil {
		return call
	}
	if strings.TrimSpace(stringArg(args["timezone"])) == "" {
		args["timezone"] = timezone
		call.Arguments = args
	}
	return call
}

func defaultTimezoneArgs(timezone string) map[string]any {
	timezone = strings.TrimSpace(timezone)
	if timezone == "" {
		return map[string]any{}
	}
	return map[string]any{"timezone": timezone}
}

func validateRequiredToolArguments(call tools.ToolCall) (tools.ToolCall, error) {
	switch call.Name {
	case toolNameLocalTime, toolNameCalendar:
		args, err := toolObjectArgs(call)
		if err != nil {
			return call, err
		}
		call.Arguments = args
		return call, nil
	case toolNameCalculate:
		args, err := toolObjectArgs(call)
		if err != nil {
			return call, err
		}
		if strings.TrimSpace(stringArg(args["expression"])) == "" {
			return call, fmt.Errorf("calculate expression is required")
		}
		call.Arguments = args
		return call, nil
	case toolNameWebSearch:
		args, err := toolObjectArgs(call)
		if err != nil {
			return call, err
		}
		if strings.TrimSpace(stringArg(args["query"])) == "" {
			return call, fmt.Errorf("web_search query is required")
		}
		call.Arguments = args
		return call, nil
	case toolNameWebRead:
		args, err := toolObjectArgs(call)
		if err != nil {
			return call, err
		}
		urls := stringSliceArg(args["urls"])
		if len(urls) == 0 {
			return call, fmt.Errorf("web_read urls are required")
		}
		for _, rawURL := range urls {
			parsed, err := url.Parse(strings.TrimSpace(rawURL))
			if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
				return call, fmt.Errorf("web_read URL %q is invalid", rawURL)
			}
		}
		args["urls"] = urls
		call.Arguments = args
		return call, nil
	default:
		return call, nil
	}
}

func toolObjectArgs(call tools.ToolCall) (map[string]any, error) {
	args, err := toolArgsMap(call.Arguments)
	if err != nil {
		return nil, fmt.Errorf("%s arguments must be a valid JSON object: %w", strings.TrimSpace(call.Name), err)
	}
	return args, nil
}

func sanitizeToolCallArguments(call tools.ToolCall, policy guardrails.ToolPolicy) (tools.ToolCall, error) {
	if len(guardrails.NormalizeDomains(policy.AllowedDomains)) == 0 {
		return call, nil
	}
	switch call.Name {
	case toolNameWebSearch:
		return sanitizeWebSearchArguments(call, policy)
	case toolNameWebRead:
		return sanitizeWebReadArguments(call, policy)
	default:
		return call, nil
	}
}

func sanitizeWebSearchArguments(call tools.ToolCall, policy guardrails.ToolPolicy) (tools.ToolCall, error) {
	args, err := toolArgsMap(call.Arguments)
	if err != nil {
		return call, err
	}
	requested := stringSliceArg(args["include_domains"])
	allowed := guardrails.NormalizeDomains(policy.AllowedDomains)
	if len(requested) == 0 {
		args["include_domains"] = limitedDomains(allowed, 8)
		call.Arguments = args
		return call, nil
	}
	filtered := make([]string, 0, len(requested))
	for _, domain := range requested {
		domain = guardrails.NormalizeDomain(domain)
		if guardrails.DomainAllowed(policy, domain) {
			filtered = append(filtered, domain)
		}
	}
	filtered = limitedDomains(normalizeStringList(filtered), 8)
	if len(filtered) == 0 {
		return call, fmt.Errorf("web_search include_domains are outside the allowed domain policy")
	}
	args["include_domains"] = filtered
	call.Arguments = args
	return call, nil
}

func sanitizeWebReadArguments(call tools.ToolCall, policy guardrails.ToolPolicy) (tools.ToolCall, error) {
	args, err := toolArgsMap(call.Arguments)
	if err != nil {
		return call, err
	}
	for _, rawURL := range stringSliceArg(args["urls"]) {
		domain := domainFromURL(rawURL)
		if domain == "" {
			return call, fmt.Errorf("web_read URL %q is invalid", rawURL)
		}
		if !guardrails.DomainAllowed(policy, domain) {
			return call, fmt.Errorf("web_read URL %q is outside the allowed domain policy", rawURL)
		}
	}
	return call, nil
}

func toolArgsMap(args any) (map[string]any, error) {
	data, err := normalizeToolArgsJSON(args)
	if err != nil {
		return nil, err
	}
	decoded := map[string]any{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return nil, err
	}
	return decoded, nil
}

func normalizeToolArgsJSON(args any) ([]byte, error) {
	if args == nil {
		return []byte(`{}`), nil
	}
	switch value := args.(type) {
	case json.RawMessage:
		text := strings.TrimSpace(string(value))
		if text == "" || text == "null" {
			return []byte(`{}`), nil
		}
		return []byte(text), nil
	case []byte:
		text := strings.TrimSpace(string(value))
		if text == "" || text == "null" {
			return []byte(`{}`), nil
		}
		return []byte(text), nil
	case string:
		text := strings.TrimSpace(value)
		if text == "" {
			return []byte(`{}`), nil
		}
		if json.Valid([]byte(text)) {
			return []byte(text), nil
		}
		encoded, err := json.Marshal(text)
		if err != nil {
			return nil, err
		}
		return encoded, nil
	default:
		data, err := json.Marshal(args)
		if err != nil {
			return nil, err
		}
		text := strings.TrimSpace(string(data))
		if text == "" || text == "null" {
			return []byte(`{}`), nil
		}
		return []byte(text), nil
	}
}

func stringSliceArg(value any) []string {
	switch typed := value.(type) {
	case []string:
		return normalizeStringList(typed)
	case []any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				values = append(values, text)
			}
		}
		return normalizeStringList(values)
	default:
		return nil
	}
}

func stringArg(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func splitTextRunes(text string, maxRunes int) []string {
	if text == "" {
		return nil
	}
	if maxRunes <= 0 {
		maxRunes = len([]rune(text))
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

func limitedDomains(domains []string, limit int) []string {
	domains = guardrails.NormalizeDomains(domains)
	if limit > 0 && len(domains) > limit {
		return domains[:limit]
	}
	return domains
}

func domainFromURL(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	return guardrails.NormalizeDomain(parsed.Hostname())
}

func blockedToolResult(call tools.ToolCall, err error) string {
	payload := map[string]any{
		"ok":     false,
		"error":  "tool call blocked by runtime policy",
		"tool":   strings.TrimSpace(call.Name),
		"callId": strings.TrimSpace(call.ID),
	}
	if err != nil {
		payload["policy_error"] = err.Error()
	}
	data, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		return `{"ok":false,"error":"tool call blocked by runtime policy"}`
	}
	return string(data)
}

func toolResultError(result string) string {
	var payload struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		return "tool returned an invalid result"
	}
	return strings.TrimSpace(payload.Error)
}

func newToolCatalog(definitions []tools.ToolDefinition) map[string]tools.ToolDefinition {
	catalog := map[string]tools.ToolDefinition{}
	for _, definition := range definitions {
		name := strings.TrimSpace(definition.Name)
		if name == "" {
			continue
		}
		catalog[name] = definition
	}
	return catalog
}

func effectiveToolPolicy(policy guardrails.ToolPolicy, definitions []tools.ToolDefinition) guardrails.ToolPolicy {
	if len(policy.AllowedTools) > 0 {
		policy.AllowedTools = normalizeStringList(policy.AllowedTools)
		return policy
	}
	allowed := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		if name := strings.TrimSpace(definition.Name); name != "" {
			allowed = append(allowed, name)
		}
	}
	policy.AllowedTools = normalizeStringList(allowed)
	return policy
}

func reportToolAudit(req Request, event ToolAuditEvent) error {
	if req.OnToolAudit == nil {
		return nil
	}
	event.RunID = strings.TrimSpace(req.RunID)
	event.AgentID = strings.TrimSpace(req.AgentID)
	event.InvokerUserID = strings.TrimSpace(req.InvokerUserID)
	event.ScopeType = strings.TrimSpace(req.ScopeType)
	event.ScopeID = strings.TrimSpace(req.ScopeID)
	event.Tool = strings.TrimSpace(event.Tool)
	event.CallID = strings.TrimSpace(event.CallID)
	event.Status = strings.TrimSpace(event.Status)
	event.Error = strings.TrimSpace(event.Error)
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	return req.OnToolAudit(event)
}

func isDeferredToolAnswer(content string) bool {
	normalized := strings.ToLower(strings.TrimSpace(content))
	if normalized == "" {
		return false
	}
	deferredPhrases := []string{
		"我帮你查一下",
		"我来查一下",
		"我先查",
		"我先搜索",
		"先搜索",
		"正在搜索",
		"请稍等",
		"稍等",
		"我会搜索",
		"我将搜索",
		"我会查",
		"我将查",
		"我需要搜索",
		"让我搜索",
		"让我查",
		"i'll search",
		"i will search",
		"let me search",
		"let me check",
		"i'll check",
		"i will check",
		"i'll look up",
		"i will look up",
		"please wait",
		"one moment",
	}
	for _, phrase := range deferredPhrases {
		if strings.Contains(normalized, phrase) {
			return true
		}
	}
	return false
}

func finalAnswerNeedsRegeneration(content string) bool {
	return isDeferredToolAnswer(content) || containsInternalToolMarkup(content)
}

func containsInternalToolMarkup(content string) bool {
	normalized := compactToolMarkupText(content)
	if normalized == "" {
		return false
	}
	markers := []string{
		"<|dsml|",
		"<|tool_calls|",
		"<|tool_call|",
		"</|tool_calls|",
		"tool_calls>",
		"<tool_call",
		"</tool_call",
		"invokename=",
		"function_call",
		"\"tool_calls\"",
		"'tool_calls'",
	}
	for _, marker := range markers {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func compactToolMarkupText(content string) string {
	replacer := strings.NewReplacer(
		" ", "",
		"\n", "",
		"\r", "",
		"\t", "",
		"｜", "|",
		"＜", "<",
		"＞", ">",
	)
	return strings.ToLower(replacer.Replace(strings.TrimSpace(content)))
}

func fallbackFinalAnswerCompletion(messages []model.ModelMessage, completion model.ModelResponse) model.ModelResponse {
	completion.Content = fallbackFinalAnswerText(messages)
	completion.ToolCalls = nil
	if strings.TrimSpace(completion.FinishReason) == "" || completion.FinishReason == "tool_calls" {
		completion.FinishReason = "stop"
	}
	return completion
}

func fallbackFinalAnswerText(messages []model.ModelMessage) string {
	if prefersChinese(lastUserMessage(messages)) {
		return "这次检索流程没有生成可用的核验证据，我无法可靠回答这个需要实时确认的问题。请稍后重试，或提供可核对的来源/数据后我再继续分析。"
	}
	return "The search flow did not produce usable verification evidence, so I cannot reliably answer this live-information question. Please try again later or provide sources/data I can analyze."
}

func executeTool(ctx context.Context, executor ToolExecutor, call tools.ToolCall) string {
	if executor == nil {
		return `{"ok":false,"error":"tool executor unavailable"}`
	}
	result := executor(ctx, call)
	if strings.TrimSpace(result) == "" {
		return `{"ok":false,"error":"tool returned an empty result"}`
	}
	return result
}

func reportProgress(report ProgressReporter, progress Progress) error {
	if report == nil {
		return nil
	}
	progress.StepID = strings.TrimSpace(progress.StepID)
	progress.Phase = strings.TrimSpace(progress.Phase)
	progress.Status = strings.TrimSpace(progress.Status)
	progress.Message = strings.TrimSpace(progress.Message)
	progress.Narration = strings.TrimSpace(progress.Narration)
	if progress.Narration != "" {
		progress.Narration += "\n"
	}
	progress.Tool = strings.TrimSpace(progress.Tool)
	progress.Query = strings.TrimSpace(progress.Query)
	progress.URLs = normalizeStringList(progress.URLs)
	return report(progress)
}

func cloneMessages(messages []model.ModelMessage) []model.ModelMessage {
	cloned := make([]model.ModelMessage, len(messages))
	copy(cloned, messages)
	return cloned
}

func normalizeToolCallIDs(calls []tools.ToolCall) []tools.ToolCall {
	normalized := make([]tools.ToolCall, 0, len(calls))
	for index, call := range calls {
		if strings.TrimSpace(call.ID) == "" {
			call.ID = fmt.Sprintf("tool_call_%d", index+1)
		}
		normalized = append(normalized, call)
	}
	return normalized
}

func lastUserMessage(messages []model.ModelMessage) string {
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role == "user" {
			return strings.TrimSpace(messages[index].Content)
		}
	}
	return ""
}

func toolQuery(args any) string {
	var input struct {
		Query string `json:"query"`
	}
	_ = mapToolArgs(args, &input)
	return strings.TrimSpace(input.Query)
}

func toolURLs(args any) []string {
	var input struct {
		URLs []string `json:"urls"`
	}
	_ = mapToolArgs(args, &input)
	return normalizeStringList(input.URLs)
}

func toolExpression(args any) string {
	var input struct {
		Expression string `json:"expression"`
	}
	_ = mapToolArgs(args, &input)
	return strings.TrimSpace(input.Expression)
}

func toolCalendarOperation(args any) string {
	var input struct {
		Operation string `json:"operation"`
		Date      string `json:"date"`
		StartDate string `json:"start_date"`
		EndDate   string `json:"end_date"`
	}
	_ = mapToolArgs(args, &input)
	parts := []string{strings.TrimSpace(input.Operation)}
	for _, value := range []string{input.Date, input.StartDate, input.EndDate} {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

func resultURLs(result string, limit int) []string {
	if limit <= 0 {
		return nil
	}
	var decoded struct {
		Results []struct {
			URL string `json:"url"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(result), &decoded); err != nil {
		return nil
	}
	urls := make([]string, 0, len(decoded.Results))
	for _, item := range decoded.Results {
		urls = append(urls, item.URL)
	}
	urls = normalizeStringList(urls)
	if len(urls) > limit {
		urls = urls[:limit]
	}
	return urls
}

func searchResultQuery(result string) string {
	var decoded struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal([]byte(result), &decoded); err != nil {
		return ""
	}
	return strings.TrimSpace(decoded.Query)
}

func resultSummary(result string) (bool, int) {
	var payload struct {
		OK          bool `json:"ok"`
		ResultCount int  `json:"result_count"`
	}
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		return false, 0
	}
	return payload.OK, payload.ResultCount
}

func webSearchProviderUnavailable(result string) bool {
	var payload struct {
		OK             bool   `json:"ok"`
		EvidenceStatus string `json:"evidence_status"`
		Error          string `json:"error"`
	}
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		return false
	}
	if payload.OK {
		return false
	}
	status := strings.ToLower(strings.TrimSpace(payload.EvidenceStatus))
	if status == "provider_unavailable" {
		return true
	}
	errorText := strings.ToLower(strings.TrimSpace(payload.Error))
	return strings.Contains(errorText, "context deadline exceeded") ||
		strings.Contains(errorText, "client.timeout exceeded") ||
		strings.Contains(errorText, "web search failed across providers") ||
		strings.Contains(errorText, "rate limit") ||
		strings.Contains(errorText, "returned 429")
}

func toolResultOK(result string) bool {
	var payload struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		return false
	}
	return payload.OK
}

func mapToolArgs(args any, target any) error {
	data, err := json.Marshal(args)
	if err != nil {
		return err
	}
	if len(data) == 0 || string(data) == "null" {
		data = []byte(`{}`)
	}
	return json.Unmarshal(data, target)
}

func normalizeStringList(values []string) []string {
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

func normalizedRoutingMaxTokens(value int) int {
	if value > 0 {
		return value
	}
	return defaultRoutingMaxTokens
}

func normalizedMaxOutputTokens(value int) int {
	if value > 0 {
		return value
	}
	return defaultResponseTokens
}

func normalizedMaxIterations(value int) int {
	if value > 0 {
		return value
	}
	return defaultMaxTAOIterations
}

func normalizedMaxToolCalls(value int) int {
	if value > 0 {
		return value
	}
	return defaultMaxToolCalls
}

func timeoutContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, timeout)
}

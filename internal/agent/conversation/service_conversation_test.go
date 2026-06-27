package conversation

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"nlserver/internal/agent/tools"
	"nlserver/internal/config"
	"nlserver/internal/repository"
)

func TestAIConversationDateThenNasdaqWebSearch(t *testing.T) {
	pool := newAIServicePostgresIntegrationPool(t)
	repos := repository.New(pool)
	ctx := context.Background()

	userID := "11111111-1111-1111-1111-111111111111"
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, public_id, email, password_hash, nick)
		VALUES ($1::uuid, 'aitestuser', 'aitest@example.com', 'hash', 'AI Test')`,
		userID,
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	searchServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"query": "昨天晚上纳斯达克100走势",
			"answer": "纳斯达克100指数昨晚整体上涨，科技权重股带动指数走高。",
			"results": [
				{"title": "Nasdaq 100 market recap", "url": "https://example.com/nasdaq100", "content": "Nasdaq 100 finished higher overnight.", "score": 0.92}
			],
			"response_time": 0.11
		}`))
	}))
	defer searchServer.Close()
	previousEndpoint := tavilySearchEndpoint
	tavilySearchEndpoint = searchServer.URL
	t.Cleanup(func() {
		tavilySearchEndpoint = previousEndpoint
	})

	client := &conversationFlowClient{t: t}
	service := NewService(config.AIConfig{
		DefaultModel:     "deepseek-v4-pro",
		TavilyAPIKey:     "test-tavily-key",
		RequestTimeout:   time.Second,
		MaxContextTokens: 0,
	}, repos, pool, client)
	conversation, err := service.CreateConversation(ctx, CreateConversationInput{
		UserID: userID,
		Model:  "deepseek-v4-pro",
	})
	if err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}

	var firstChunks []string
	first, err := service.StreamMessage(ctx, StreamMessageInput{
		SendMessageInput: SendMessageInput{
			UserID:          userID,
			ConversationID:  conversation.ID,
			Message:         "今天日期是？",
			Model:           "deepseek-v4-pro",
			ThinkingType:    "enabled",
			ReasoningEffort: "high",
		},
		OnDelta: func(delta StreamMessageDelta) error {
			if delta.Transient {
				return nil
			}
			firstChunks = append(firstChunks, delta.Content)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("first StreamMessage() error = %v", err)
	}
	firstText := strings.Join(firstChunks, "")
	if !strings.Contains(firstText, time.Now().Format("2006-01-02")) {
		t.Fatalf("first streamed text = %q, want current date", firstText)
	}
	if !client.sawLocalTimeContext {
		t.Fatal("model client did not receive local_time tool context")
	}
	if first.AssistantMessage.Text != firstText {
		t.Fatalf("first assistant persisted text = %q, streamed = %q", first.AssistantMessage.Text, firstText)
	}

	var secondChunks []string
	second, err := service.StreamMessage(ctx, StreamMessageInput{
		SendMessageInput: SendMessageInput{
			UserID:          userID,
			ConversationID:  conversation.ID,
			Message:         "昨天晚上纳斯达克100走势",
			Model:           "deepseek-v4-pro",
			ThinkingType:    "enabled",
			ReasoningEffort: "high",
		},
		OnDelta: func(delta StreamMessageDelta) error {
			if delta.Transient {
				return nil
			}
			secondChunks = append(secondChunks, delta.Content)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("second StreamMessage() error = %v", err)
	}
	secondText := strings.Join(secondChunks, "")
	for _, want := range []string{"纳斯达克100", "上涨", "https://example.com/nasdaq100"} {
		if !strings.Contains(secondText, want) {
			t.Fatalf("second streamed text = %q, want contains %q", secondText, want)
		}
	}
	if second.AssistantMessage.Text != secondText {
		t.Fatalf("second assistant persisted text = %q, streamed = %q", second.AssistantMessage.Text, secondText)
	}
	if !client.sawSearchContext {
		t.Fatal("model client did not receive web_search tool context")
	}
}

type conversationFlowClient struct {
	t                   *testing.T
	sawLocalTimeContext bool
	sawSearchContext    bool
}

func (c *conversationFlowClient) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	if len(req.Tools) > 0 {
		userText := lastUserTextForTest(req.Messages)
		switch {
		case strings.Contains(userText, "今天日期"):
			if !testMessagesHaveToolCall(req.Messages, toolNameLocalTime) {
				return CompletionResponse{ToolCalls: []tools.ToolCall{{
					ID:        "call_time",
					Name:      toolNameLocalTime,
					Arguments: map[string]any{"timezone": "Asia/Shanghai"},
				}}, FinishReason: "tool_calls"}, nil
			}
		case strings.Contains(userText, "纳斯达克100"):
			if !testMessagesHaveToolCall(req.Messages, toolNameLocalTime) {
				return CompletionResponse{ToolCalls: []tools.ToolCall{{
					ID:        "call_time",
					Name:      toolNameLocalTime,
					Arguments: map[string]any{"timezone": "Asia/Shanghai"},
				}}, FinishReason: "tool_calls"}, nil
			}
			if !testMessagesHaveToolCall(req.Messages, toolNameWebSearch) {
				return CompletionResponse{ToolCalls: []tools.ToolCall{{
					ID:   "call_search",
					Name: toolNameWebSearch,
					Arguments: map[string]any{
						"query":       time.Now().AddDate(0, 0, -1).Format("2006-01-02") + " 纳斯达克100 走势",
						"time_range":  "day",
						"max_results": 5,
					},
				}}, FinishReason: "tool_calls"}, nil
			}
		}
	}
	contextText := joinedMessageContentForTest(req.Messages)
	switch {
	case messagesContainToolEvidence(req.Messages, toolNameWebSearch):
		if !strings.Contains(contextText, "https://example.com/nasdaq100") {
			c.t.Fatalf("complete request missing web_search source context: %s", contextText)
		}
		c.sawSearchContext = true
		answer := "昨晚纳斯达克100整体上涨，科技权重股带动指数走高。来源：https://example.com/nasdaq100"
		return CompletionResponse{
			Content:      answer,
			TokenCount:   roughTokenCount(answer),
			FinishReason: "stop",
		}, nil
	case messagesContainToolEvidence(req.Messages, toolNameLocalTime):
		c.sawLocalTimeContext = true
		answer := "今天是 " + time.Now().Format("2006-01-02") + "。"
		return CompletionResponse{
			Content:      answer,
			TokenCount:   roughTokenCount(answer),
			FinishReason: "stop",
		}, nil
	}
	return CompletionResponse{
		Content:      "日期与市场走势",
		TokenCount:   6,
		FinishReason: "stop",
	}, nil
}

func joinedMessageContentForTest(messages []ChatMessage) string {
	var joined strings.Builder
	for _, message := range messages {
		joined.WriteString(message.Content)
		joined.WriteString("\n")
	}
	return joined.String()
}

func lastUserTextForTest(messages []ChatMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return messages[i].Content
		}
	}
	return ""
}

func testMessagesHaveToolCall(messages []ChatMessage, name string) bool {
	for _, message := range messages {
		for _, call := range message.ToolCalls {
			if call.Name == name {
				return true
			}
		}
	}
	return false
}

func (c *conversationFlowClient) Stream(ctx context.Context, req CompletionRequest, onDelta func(CompletionStreamDelta) error) (CompletionResponse, error) {
	var joined strings.Builder
	for _, message := range req.Messages {
		joined.WriteString(message.Content)
		joined.WriteString("\n")
	}
	contextText := joined.String()
	if req.ThinkingType != "disabled" || req.ReasoningEffort != "low" {
		c.t.Fatalf("tool-augmented stream reasoning = (%q, %q), want disabled/low", req.ThinkingType, req.ReasoningEffort)
	}
	var answer string
	switch {
	case messagesContainToolEvidence(req.Messages, toolNameWebSearch):
		if !strings.Contains(contextText, "https://example.com/nasdaq100") {
			c.t.Fatalf("stream request missing web_search source context: %s", contextText)
		}
		c.sawSearchContext = true
		answer = "昨晚纳斯达克100整体上涨，科技权重股带动指数走高。来源：https://example.com/nasdaq100"
	case messagesContainToolEvidence(req.Messages, toolNameLocalTime):
		c.sawLocalTimeContext = true
		answer = "今天是 " + time.Now().Format("2006-01-02") + "。"
	default:
		c.t.Fatalf("stream request missing backend tool context: %s", contextText)
	}
	if onDelta != nil {
		if err := onDelta(CompletionStreamDelta{Content: answer}); err != nil {
			return CompletionResponse{}, err
		}
	}
	return CompletionResponse{
		Content:      answer,
		TokenCount:   roughTokenCount(answer),
		FinishReason: "stop",
	}, nil
}

func newAIServicePostgresIntegrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := strings.TrimSpace(os.Getenv("NETHERLINK_TEST_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("set NETHERLINK_TEST_POSTGRES_DSN to run PostgreSQL integration tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	adminPool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect PostgreSQL test database: %v", err)
	}

	schema := fmt.Sprintf("nltest_ai_%d", time.Now().UnixNano())
	if _, err := adminPool.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, _ = adminPool.Exec(cleanupCtx, `DROP SCHEMA IF EXISTS `+schema+` CASCADE`)
		adminPool.Close()
	})

	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse PostgreSQL test DSN: %v", err)
	}
	poolConfig.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	if poolConfig.ConnConfig.RuntimeParams == nil {
		poolConfig.ConnConfig.RuntimeParams = map[string]string{}
	}
	poolConfig.ConnConfig.RuntimeParams["search_path"] = schema + ",public"

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatalf("connect PostgreSQL test schema: %v", err)
	}
	t.Cleanup(pool.Close)

	for _, script := range []string{"01_extensions.sql", "02_schema.sql"} {
		sql, err := os.ReadFile(aiServiceTestSchemaPath(t, script))
		if err != nil {
			t.Fatalf("read %s: %v", script, err)
		}
		if _, err := pool.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("apply %s: %v", script, err)
		}
	}

	return pool
}

func aiServiceTestSchemaPath(t *testing.T, name string) string {
	t.Helper()

	for _, root := range []string{".", "..", "../.."} {
		path := filepath.Join(root, "db", "init", name)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	t.Fatalf("schema script %s not found", name)
	return ""
}

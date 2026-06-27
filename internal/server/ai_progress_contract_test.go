package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"nlserver/internal/repository"
)

func TestAIProgressEventVisibilityContract(t *testing.T) {
	thinking, err := json.Marshal(aiStreamThinkingResponse{
		StreamID:    "ais_test",
		Active:      true,
		UserVisible: false,
		StepID:      "step-1",
		Sequence:    1,
		Phase:       "searching",
		Status:      "running",
		Message:     "正在搜索相关来源",
		Tool:        "web_search",
		Query:       "京能电力 2026 一季报",
	})
	if err != nil {
		t.Fatalf("marshal thinking payload: %v", err)
	}
	thinkingJSON := string(thinking)
	if !strings.Contains(thinkingJSON, `"userVisible":false`) {
		t.Fatalf("thinking payload missing userVisible=false: %s", thinkingJSON)
	}
	for _, expected := range []string{`"stepId":"step-1"`, `"sequence":1`, `"message":"正在搜索相关来源"`} {
		if !strings.Contains(thinkingJSON, expected) {
			t.Fatalf("thinking payload missing %s: %s", expected, thinkingJSON)
		}
	}

	chunk, err := json.Marshal(aiStreamChunkResponse{
		StreamID:     "ais_test",
		Delta:        "我先查",
		Kind:         "progress",
		Transient:    true,
		StepID:       "step-1",
		SegmentID:    "progress-1",
		SegmentStart: true,
	})
	if err != nil {
		t.Fatalf("marshal chunk payload: %v", err)
	}
	if !strings.Contains(string(chunk), `"transient":true`) {
		t.Fatalf("chunk payload missing transient=true: %s", chunk)
	}
	for _, expected := range []string{`"kind":"progress"`, `"stepId":"step-1"`, `"segmentId":"progress-1"`, `"segmentStart":true`} {
		if !strings.Contains(string(chunk), expected) {
			t.Fatalf("chunk payload missing %s: %s", expected, chunk)
		}
	}

	answer, err := json.Marshal(aiStreamChunkResponse{
		StreamID:  "ais_test",
		Delta:     "根据",
		Kind:      "answer",
		Transient: false,
	})
	if err != nil {
		t.Fatalf("marshal answer chunk: %v", err)
	}
	answerJSON := string(answer)
	if !strings.Contains(answerJSON, `"kind":"answer"`) || !strings.Contains(answerJSON, `"transient":false`) {
		t.Fatalf("answer chunk contract = %s", answerJSON)
	}
}

func TestAIMessageResponseIncludesCollapsibleTrace(t *testing.T) {
	now := time.Now()
	response := makeAIMessageResponse(repository.AIMessage{
		ID:             "message-1",
		ConversationID: "conversation-1",
		Role:           "assistant",
		Text:           "最终回答",
		Trace: &repository.AIMessageTrace{
			ID:         "trace-1",
			MessageID:  "message-1",
			Status:     "completed",
			Summary:    "检索并读取了财报来源",
			Steps:      json.RawMessage(`[{"sequence":1,"phase":"searching","status":"completed","text":"已找到来源"}]`),
			SourceRefs: json.RawMessage(`["https://example.com/report"]`),
			CreatedAt:  now,
			UpdatedAt:  now,
		},
		CreatedAt: now,
		UpdatedAt: now,
	})
	if response.Trace == nil {
		t.Fatal("response trace is nil")
	}
	if response.Trace.TraceID != "trace-1" || response.Trace.Summary != "检索并读取了财报来源" {
		t.Fatalf("response trace = %#v", response.Trace)
	}
	if !strings.Contains(string(response.Trace.Steps), `"phase":"searching"`) {
		t.Fatalf("response trace steps = %s", response.Trace.Steps)
	}
}

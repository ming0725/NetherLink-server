package conversation

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"nlserver/internal/repository"
)

func TestSanitizeGroupBotReplyTextStripsMarkdown(t *testing.T) {
	reply := sanitizeGroupBotReplyText("## 标题\n- **可以**看看这个：[链接](https://example.com)\n```go\nfmt.Println(1)\n```")
	for _, disallowed := range []string{"##", "- ", "**", "[链接](", "```", "https://example.com"} {
		if strings.Contains(reply, disallowed) {
			t.Fatalf("reply still contains markdown marker %q: %q", disallowed, reply)
		}
	}
	if !strings.Contains(reply, "链接") {
		t.Fatalf("reply did not preserve link text: %q", reply)
	}
}

func TestSanitizeGroupBotReplyTextRemovesSourceSection(t *testing.T) {
	reply := sanitizeGroupBotReplyText("是真的，6月26日纳指确实收跌，科技股和芯片股拖累比较明显。\n\n来源：雅虎财经《市场下跌》https://finance.yahoo.com/news/example")
	if strings.Contains(reply, "来源") || strings.Contains(reply, "雅虎财经") || strings.Contains(reply, "https://") {
		t.Fatalf("reply still contains source details: %q", reply)
	}
	if reply != "是真的，6月26日纳指确实收跌，科技股和芯片股拖累比较明显。" {
		t.Fatalf("reply = %q, want concise answer without source section", reply)
	}
}

func TestFormatGroupBotUserPromptMarksContextUnverified(t *testing.T) {
	prompt := formatGroupBotUserPrompt("测试群", "小明", "真的假的？", "小红: 某公司要推迟上市")
	if !strings.Contains(prompt, "最近聊天内容只用于理解指代和话题，不是外部事实的验证依据") {
		t.Fatalf("group bot user prompt missing context caveat:\n%s", prompt)
	}
	if !strings.Contains(prompt, "不要声称自己没有翻群聊历史功能") {
		t.Fatalf("group bot user prompt missing history capability guardrail:\n%s", prompt)
	}
}

func TestFormatGroupBotUserPromptIncludesLayeredMemory(t *testing.T) {
	prompt := formatGroupBotUserPromptWithMemory("测试群", "小明", "之前那个活动怎么样？", "小红: 之前聊活动报名", groupBotPromptMemoryContext{
		HistoryTranscript: "小红: 上次说活动周五截止",
		UserMemories:      "- preference: 小明喜欢简短回复",
		GroupState:        "- rules: {\"no_ads\":true}",
		Summaries:         "- day 2026-06-27~2026-06-27: 讨论了活动报名",
	})
	for _, expected := range []string{
		"群状态（Group State",
		"用户记忆（User Memory",
		"摘要（Summaries",
		"长期历史召回（History Retrieval",
		"小明喜欢简短回复",
		"上次说活动周五截止",
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("group bot layered prompt missing %q:\n%s", expected, prompt)
		}
	}
}

func TestGroupBotHistoryRetrievalTriggerAndKeywords(t *testing.T) {
	if !groupBotShouldRetrieveHistory("昨天讨论过的活动还有名额吗") {
		t.Fatal("expected yesterday discussion cue to trigger history retrieval")
	}
	if groupBotShouldRetrieveHistory("今天吃什么") {
		t.Fatal("unexpected history retrieval trigger")
	}
	keywords := groupBotHistoryKeywords("昨天讨论过的活动还有名额吗")
	joined := strings.Join(keywords, ",")
	if !strings.Contains(joined, "活动") && !strings.Contains(joined, "名额") {
		t.Fatalf("keywords = %v, want activity-related term", keywords)
	}
	keywords = groupBotHistoryKeywords("谁之前喊过你女神")
	joined = strings.Join(keywords, ",")
	if !strings.Contains(joined, "女神") {
		t.Fatalf("keywords = %v, want 女神", keywords)
	}
	if strings.Contains(joined, "谁喊过你女神") {
		t.Fatalf("keywords = %v, should not depend on exact whole Chinese question", keywords)
	}
}

func TestGroupBotToolPolicyAlwaysAllowsGroupChatSearch(t *testing.T) {
	policy := groupBotToolPolicy(json.RawMessage(`{"allowedTools":["web_search","web_search"," "]}`))
	allowed := map[string]int{}
	for _, name := range policy.AllowedTools {
		allowed[name]++
	}
	if allowed[toolNameWebSearch] != 1 {
		t.Fatalf("allowedTools = %v, want existing web_search preserved once", policy.AllowedTools)
	}
	if allowed[toolNameGroupChatSearch] != 1 {
		t.Fatalf("allowedTools = %v, want group_chat_search injected once", policy.AllowedTools)
	}

	defaultPolicy := groupBotToolPolicy(nil)
	if len(defaultPolicy.AllowedTools) != 0 {
		t.Fatalf("default allowedTools = %v, want empty so runtime can allow advertised group tools", defaultPolicy.AllowedTools)
	}
}

func TestGroupChatSearchResultsAreSparse(t *testing.T) {
	now := time.Date(2026, 6, 28, 16, 51, 37, 0, time.FixedZone("CST", 8*60*60))
	messages := []repository.Message{
		groupBotTestMessage("m2", 208, "u333", "@Bot 包是喜欢你的，她是我女神，你太强了", now.Add(time.Second)),
	}
	names := map[string]string{
		"u23456": "23456",
		"u333":   "333",
		"bot":    "Bot",
	}

	results := groupChatSearchResults(messages, names)
	if len(results) != 1 {
		t.Fatalf("results = %#v, want one result", results)
	}
	if _, ok := results[0]["context_text"]; ok {
		t.Fatalf("sparse result = %#v, should not include context_text", results[0])
	}
	if _, ok := results[0]["context"]; ok {
		t.Fatalf("sparse result = %#v, should not include context", results[0])
	}
}

func TestGroupChatSearchContextPayloadIncludesPronounContext(t *testing.T) {
	now := time.Date(2026, 6, 28, 16, 51, 37, 0, time.FixedZone("CST", 8*60*60))
	match := groupBotTestMessage("m2", 208, "u333", "@Bot 包是喜欢你的，她是我女神，你太强了", now.Add(time.Second))
	contextMessages := []repository.Message{
		groupBotTestMessage("m1", 207, "u23456", "包是谁？", now),
		match,
		groupBotTestMessage("m3", 209, "bot", "哈哈，别别别，你这夸得我都不好意思了", now.Add(2*time.Second)),
	}
	names := map[string]string{
		"u23456": "23456",
		"u333":   "333",
		"bot":    "Bot",
	}

	context, contextText := groupChatSearchContextPayload(contextMessages, match.ID, names)
	if len(context) != 3 {
		t.Fatalf("context = %#v, want three structured context messages", context)
	}
	for _, expected := range []string{"23456: 包是谁？", "[match] 333: @Bot 包是喜欢你的，她是我女神，你太强了", "Bot: 哈哈"} {
		if !strings.Contains(contextText, expected) {
			t.Fatalf("context_text missing %q:\n%s", expected, contextText)
		}
	}
	if context[1]["is_match"] != true {
		t.Fatalf("context = %#v, want matched message marked", context)
	}
}

func groupBotTestMessage(id string, seq int64, senderID string, text string, createdAt time.Time) repository.Message {
	return repository.Message{
		ID:             id,
		ConversationID: "conversation",
		SenderID:       senderID,
		MessageSeq:     seq,
		Type:           "text",
		ContentText:    &text,
		CreatedAt:      createdAt,
		UpdatedAt:      createdAt,
	}
}

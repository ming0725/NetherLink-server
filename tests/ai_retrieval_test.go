package tests

import (
	"fmt"
	"strings"
	"testing"

	ai "nlserver/internal/agent/conversation"
	"nlserver/internal/repository"
)

func TestAISelectRelevantChunksRanksSemanticTermsAndKeepsNeighbors(t *testing.T) {
	chunks := make([]repository.AIFileChunk, 0, 12)
	for index := 0; index < 12; index++ {
		content := fmt.Sprintf("General onboarding note %d with account setup details.", index)
		if index == 6 {
			content = "Refund policy: cancellation requests within seven days receive full reimbursement."
		}
		if index == 7 {
			content = "After cancellation is approved, billing stops at the end of the current cycle."
		}
		chunks = append(chunks, repository.AIFileChunk{
			ID:         fmt.Sprintf("chunk-%d", index),
			AIFileID:   "file-1",
			ChunkIndex: index,
			Content:    content,
		})
	}

	selected := ai.SelectRelevantChunks("How does the refund cancellation policy work?", chunks)
	if len(selected) == 0 {
		t.Fatal("SelectRelevantChunks() returned no chunks")
	}

	indexes := map[int]bool{}
	var joined strings.Builder
	for _, chunk := range selected {
		indexes[chunk.ChunkIndex] = true
		joined.WriteString(chunk.Content)
		joined.WriteString("\n")
	}
	if !indexes[6] {
		t.Fatalf("selected chunk indexes = %#v, want direct refund policy hit", indexes)
	}
	if !indexes[7] {
		t.Fatalf("selected chunk indexes = %#v, want neighboring cancellation context", indexes)
	}
	if strings.Contains(joined.String(), "General onboarding note 0") {
		t.Fatalf("selected irrelevant leading chunk: %q", joined.String())
	}
}

func TestAISelectRelevantChunksFallsBackToOriginalOrderWithoutSearchTerms(t *testing.T) {
	chunks := make([]repository.AIFileChunk, 0, 10)
	for index := 0; index < 10; index++ {
		chunks = append(chunks, repository.AIFileChunk{
			ID:         fmt.Sprintf("chunk-%d", index),
			AIFileID:   "file-1",
			ChunkIndex: index,
			Content:    fmt.Sprintf("Content %d", index),
		})
	}

	selected := ai.SelectRelevantChunks("the and of", chunks)
	if len(selected) != 8 {
		t.Fatalf("len(selected) = %d, want 8", len(selected))
	}
	for index, chunk := range selected {
		if chunk.ChunkIndex != index {
			t.Fatalf("selected[%d].ChunkIndex = %d, want original order", index, chunk.ChunkIndex)
		}
	}
}

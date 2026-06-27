package context

import (
	"nlserver/internal/agent/memory"
	"nlserver/internal/agent/model"
)

type Source struct {
	SourceType string
	SourceID   string
	Title      string
	Content    string
	Metadata   map[string]any
}

type PromptContext struct {
	SystemInstructions string
	Messages           []model.ModelMessage
	Sources            []Source
	Memories           []memory.Entry
}

type ContextAssembler struct{}

func NewContextAssembler() *ContextAssembler {
	return &ContextAssembler{}
}

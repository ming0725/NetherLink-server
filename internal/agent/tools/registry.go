package tools

import "context"

type ToolDefinition struct {
	Name        string
	Description string
	Schema      any
	SideEffect  bool
}

type ToolCall struct {
	ID        string
	Name      string
	Arguments any
}

type ToolResult struct {
	CallID  string
	Name    string
	Content any
}

type Tool interface {
	Definition() ToolDefinition
	Call(ctx context.Context, run AgentRunContext, args any) (ToolResult, error)
}

type AgentRunContext struct {
	RunID         string
	AgentID       string
	InvokerUserID string
	ScopeType     string
	ScopeID       string
}

type ToolRegistry struct {
	tools map[string]Tool
}

func NewToolRegistry(tools ...Tool) *ToolRegistry {
	registry := &ToolRegistry{tools: map[string]Tool{}}
	for _, tool := range tools {
		if tool == nil {
			continue
		}
		definition := tool.Definition()
		if definition.Name == "" {
			continue
		}
		registry.tools[definition.Name] = tool
	}
	return registry
}

func (r *ToolRegistry) Get(name string) (Tool, bool) {
	if r == nil || name == "" {
		return nil, false
	}
	tool, ok := r.tools[name]
	return tool, ok
}

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrServerDisabled = errors.New("mcp: server disabled")
	ErrToolDisabled   = errors.New("mcp: tool disabled")
	ErrToolNotAllowed = errors.New("mcp: tool not allowed")
)

type Transport string

const (
	TransportHTTP  Transport = "http"
	TransportSSE   Transport = "sse"
	TransportStdio Transport = "stdio"
)

type ServerManifest struct {
	Name        string
	DisplayName string
	Transport   Transport
	Endpoint    string
	Command     string
	Args        []string
	Env         map[string]string
	Status      string
	Tools       []ToolManifest
}

type ToolManifest struct {
	Name                 string
	Description          string
	InputSchema          json.RawMessage
	OutputSchema         json.RawMessage
	SideEffect           bool
	RequiresConfirmation bool
	Status               string
}

type CallRequest struct {
	ServerName string
	ToolName   string
	Arguments  json.RawMessage
	Timeout    time.Duration
}

type CallResponse struct {
	Content json.RawMessage
}

type Client interface {
	ListTools(ctx context.Context, server ServerManifest) ([]ToolManifest, error)
	CallTool(ctx context.Context, server ServerManifest, req CallRequest) (CallResponse, error)
}

type Adapter struct {
	server ServerManifest
	tool   ToolManifest
	client Client
}

func NewAdapter(server ServerManifest, tool ToolManifest, client Client) *Adapter {
	return &Adapter{
		server: normalizeServerManifest(server),
		tool:   normalizeToolManifest(tool),
		client: client,
	}
}

type AdapterDefinition struct {
	Name        string
	Description string
	Schema      json.RawMessage
	SideEffect  bool
}

func (a *Adapter) Definition() AdapterDefinition {
	if a == nil {
		return AdapterDefinition{}
	}
	return AdapterDefinition{
		Name:        ToolName(a.server.Name, a.tool.Name),
		Description: a.tool.Description,
		Schema:      a.tool.InputSchema,
		SideEffect:  a.tool.SideEffect,
	}
}

func (a *Adapter) Call(ctx context.Context, args any) (CallResponse, error) {
	if a == nil || a.client == nil {
		return CallResponse{}, ErrToolDisabled
	}
	if strings.TrimSpace(a.server.Status) != "active" {
		return CallResponse{}, ErrServerDisabled
	}
	if strings.TrimSpace(a.tool.Status) != "active" {
		return CallResponse{}, ErrToolDisabled
	}
	encodedArgs, err := EncodeArguments(args)
	if err != nil {
		return CallResponse{}, err
	}
	response, err := a.client.CallTool(ctx, a.server, CallRequest{
		ServerName: a.server.Name,
		ToolName:   a.tool.Name,
		Arguments:  encodedArgs,
	})
	if err != nil {
		return CallResponse{}, err
	}
	return response, nil
}

func EncodeArguments(args any) (json.RawMessage, error) {
	if args == nil {
		return json.RawMessage(`{}`), nil
	}
	switch value := args.(type) {
	case json.RawMessage:
		return normalizeJSONArgumentBytes(value)
	case []byte:
		return normalizeJSONArgumentBytes(value)
	case string:
		text := strings.TrimSpace(value)
		if text == "" {
			return json.RawMessage(`{}`), nil
		}
		if json.Valid([]byte(text)) {
			return json.RawMessage(text), nil
		}
		encoded, err := json.Marshal(text)
		if err != nil {
			return nil, fmt.Errorf("mcp: encode tool arguments: %w", err)
		}
		return json.RawMessage(encoded), nil
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("mcp: encode tool arguments: %w", err)
		}
		return normalizeJSONArgumentBytes(encoded)
	}
}

func DecodeArguments(raw json.RawMessage) (any, error) {
	normalized, err := normalizeJSONArgumentBytes(raw)
	if err != nil {
		return nil, err
	}
	var args any
	if err := json.Unmarshal(normalized, &args); err != nil {
		return nil, fmt.Errorf("mcp: decode tool arguments: %w", err)
	}
	return args, nil
}

func normalizeJSONArgumentBytes(raw []byte) (json.RawMessage, error) {
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" {
		return json.RawMessage(`{}`), nil
	}
	var probe any
	if err := json.Unmarshal([]byte(text), &probe); err != nil {
		return nil, fmt.Errorf("mcp: invalid tool arguments JSON: %w", err)
	}
	return json.RawMessage(text), nil
}

func ToolName(serverName string, toolName string) string {
	serverName = strings.TrimSpace(serverName)
	toolName = strings.TrimSpace(toolName)
	if serverName == "" || toolName == "" {
		return ""
	}
	return "mcp:" + serverName + "." + toolName
}

func normalizeServerManifest(server ServerManifest) ServerManifest {
	server.Name = strings.TrimSpace(server.Name)
	server.DisplayName = strings.TrimSpace(server.DisplayName)
	server.Endpoint = strings.TrimSpace(server.Endpoint)
	server.Command = strings.TrimSpace(server.Command)
	server.Status = strings.TrimSpace(server.Status)
	if server.Status == "" {
		server.Status = "disabled"
	}
	if server.Env == nil {
		server.Env = map[string]string{}
	}
	return server
}

func normalizeToolManifest(tool ToolManifest) ToolManifest {
	tool.Name = strings.TrimSpace(tool.Name)
	tool.Description = strings.TrimSpace(tool.Description)
	tool.Status = strings.TrimSpace(tool.Status)
	if tool.Status == "" {
		tool.Status = "active"
	}
	if len(tool.InputSchema) == 0 {
		tool.InputSchema = json.RawMessage(`{}`)
	}
	if len(tool.OutputSchema) == 0 {
		tool.OutputSchema = json.RawMessage(`{}`)
	}
	return tool
}

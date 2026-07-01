package profile

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"nlserver/internal/agent/mcp"
	"nlserver/internal/agent/mcp/mcp_knowledge"
	"nlserver/internal/agent/mcp/mcp_utility"
	"nlserver/internal/agent/mcp/mcp_websearch"
	"nlserver/internal/agent/model"
	"nlserver/internal/config"
	"nlserver/internal/repository"
)

const (
	maxAgentNameLength   = 80
	maxAgentPromptLength = 20000
	maxAgentModelLength  = 128
)

var (
	ErrValidation  = errors.New("agent: validation failed")
	ErrForbidden   = errors.New("agent: forbidden")
	ErrUnavailable = errors.New("agent: unavailable")
)

type Service struct {
	repos        repository.Repositories
	tx           repository.TxBeginner
	modelGateway model.ModelGateway
	aiConfig     config.AIConfig
}

type ListProfilesInput struct {
	UserID string
	Limit  int
	Offset int
}

type CreateProfileInput struct {
	UserID       string
	Name         string
	AvatarFileID *string
	BasePrompt   string
	Model        *string
	ToolPolicy   json.RawMessage
	SafetyPolicy json.RawMessage
}

type CreateGroupBotInput struct {
	UserID            string
	GroupID           string
	Name              string
	AvatarFileID      *string
	BasePrompt        string
	Model             *string
	ToolPolicy        json.RawMessage
	SafetyPolicy      json.RawMessage
	CooldownSeconds   int
	ClientOperationID string
}

type GetProfileInput struct {
	UserID  string
	AgentID string
}

type DeleteProfileInput struct {
	UserID  string
	AgentID string
}

func NewService(repos repository.Repositories, tx repository.TxBeginner) *Service {
	return NewServiceWithModelGateway(repos, tx, nil)
}

func NewServiceWithModelGateway(repos repository.Repositories, tx repository.TxBeginner, modelGateway model.ModelGateway, aiConfig ...config.AIConfig) *Service {
	var cfg config.AIConfig
	if len(aiConfig) > 0 {
		cfg = aiConfig[0]
	}
	return &Service{repos: repos, tx: tx, modelGateway: modelGateway, aiConfig: cfg}
}

func (s *Service) CreateProfile(ctx context.Context, input CreateProfileInput) (repository.AgentProfile, error) {
	if s == nil || s.repos.Agents == nil || s.tx == nil {
		return repository.AgentProfile{}, ErrUnavailable
	}
	name := strings.TrimSpace(input.Name)
	basePrompt := strings.TrimSpace(input.BasePrompt)
	model := normalizeOptionalString(input.Model)
	if strings.TrimSpace(input.UserID) == "" ||
		name == "" ||
		len(name) > maxAgentNameLength ||
		len(basePrompt) > maxAgentPromptLength ||
		(model != nil && len(*model) > maxAgentModelLength) ||
		!validJSONObject(input.ToolPolicy) ||
		!validJSONObject(input.SafetyPolicy) {
		return repository.AgentProfile{}, ErrValidation
	}

	var profile repository.AgentProfile
	err := repository.WithTx(ctx, s.tx, func(repos repository.Repositories) error {
		botUserID, err := repos.Agents.CreateBotUser(ctx, repository.CreateAgentBotUserParams{
			Name:         name,
			AvatarFileID: input.AvatarFileID,
		})
		if err != nil {
			return err
		}
		ownerUserID := input.UserID
		created, err := repos.Agents.CreateProfile(ctx, repository.CreateAgentProfileParams{
			BotUserID:    botUserID,
			OwnerUserID:  &ownerUserID,
			Kind:         "custom",
			Visibility:   "private",
			Name:         name,
			AvatarFileID: input.AvatarFileID,
			BasePrompt:   basePrompt,
			Model:        model,
			ToolPolicy:   jsonObjectOrDefault(input.ToolPolicy),
			SafetyPolicy: jsonObjectOrDefault(input.SafetyPolicy),
		})
		if err != nil {
			return err
		}
		profile = created
		return nil
	})
	if err != nil {
		return repository.AgentProfile{}, mapRepositoryError(err)
	}
	return profile, nil
}

func (s *Service) CreateGroupBot(ctx context.Context, input CreateGroupBotInput) (repository.AgentProfile, error) {
	if s == nil || s.repos.Agents == nil || s.repos.Groups == nil || s.tx == nil {
		return repository.AgentProfile{}, ErrUnavailable
	}
	if normalizedOptional(input.AvatarFileID) != nil && s.repos.Files == nil {
		return repository.AgentProfile{}, ErrUnavailable
	}
	name := strings.TrimSpace(input.Name)
	basePrompt := strings.TrimSpace(input.BasePrompt)
	model := normalizeOptionalString(input.Model)
	if strings.TrimSpace(input.UserID) == "" ||
		strings.TrimSpace(input.GroupID) == "" ||
		name == "" ||
		len([]rune(name)) > maxAgentNameLength ||
		len([]rune(basePrompt)) > maxAgentPromptLength ||
		(model != nil && len(*model) > maxAgentModelLength) ||
		input.CooldownSeconds < 0 ||
		!validJSONObject(input.ToolPolicy) ||
		!validJSONObject(input.SafetyPolicy) {
		return repository.AgentProfile{}, ErrValidation
	}
	member, err := s.repos.Groups.GetMember(ctx, input.GroupID, input.UserID)
	if err != nil {
		return repository.AgentProfile{}, mapRepositoryError(err)
	}
	if member.Role != "owner" && member.Role != "admin" {
		return repository.AgentProfile{}, ErrForbidden
	}

	var profile repository.AgentProfile
	err = repository.WithTx(ctx, s.tx, func(repos repository.Repositories) error {
		avatarFileID := normalizedOptional(input.AvatarFileID)
		if avatarFileID != nil {
			if err := validateAgentAvatarFile(ctx, repos, *avatarFileID); err != nil {
				return err
			}
		}
		botUserID, err := repos.Agents.CreateBotUser(ctx, repository.CreateAgentBotUserParams{
			Name:         name,
			AvatarFileID: avatarFileID,
		})
		if err != nil {
			return err
		}
		if avatarFileID != nil && repos.Files != nil {
			if _, err := repos.Files.CreateUsage(ctx, repository.CreateFileUsageParams{
				FileID:      *avatarFileID,
				UsageType:   "avatar",
				BusinessID:  botUserID,
				OwnerUserID: input.UserID,
				Visibility:  "public",
			}); err != nil {
				return err
			}
		}
		ownerUserID := input.UserID
		created, err := repos.Agents.CreateProfile(ctx, repository.CreateAgentProfileParams{
			BotUserID:    botUserID,
			OwnerUserID:  &ownerUserID,
			Kind:         "group_bot",
			Visibility:   "private",
			Name:         name,
			AvatarFileID: avatarFileID,
			BasePrompt:   basePrompt,
			Model:        model,
			ToolPolicy:   jsonObjectOrDefault(input.ToolPolicy),
			SafetyPolicy: jsonObjectOrDefault(input.SafetyPolicy),
		})
		if err != nil {
			return err
		}
		if _, err := repos.Agents.CreateBinding(ctx, repository.CreateAgentBindingParams{
			AgentID:         created.ID,
			ScopeType:       "group",
			ScopeID:         input.GroupID,
			TriggerMode:     "mention",
			Enabled:         true,
			CooldownSeconds: input.CooldownSeconds,
			CreatedBy:       input.UserID,
		}); err != nil {
			return err
		}
		if err := repos.Groups.AddMembers(ctx, input.GroupID, []string{botUserID}, "ai"); err != nil {
			return err
		}
		if _, err := repos.Groups.UpdateMember(ctx, repository.UpdateGroupMemberParams{
			GroupID:     input.GroupID,
			UserID:      botUserID,
			NicknameSet: true,
			Nickname:    &name,
		}); err != nil {
			return err
		}
		if repos.Chat != nil {
			conversationID, found, err := repos.Chat.FindGroupConversation(ctx, input.GroupID)
			if err != nil {
				return err
			}
			if found {
				if err := repos.Chat.AddConversationParticipants(ctx, conversationID, []string{botUserID}); err != nil {
					return err
				}
			}
		}
		profile = created
		return nil
	})
	if err != nil {
		return repository.AgentProfile{}, mapRepositoryError(err)
	}
	return profile, nil
}

func (s *Service) ListProfiles(ctx context.Context, input ListProfilesInput) ([]repository.AgentProfile, error) {
	if s == nil || s.repos.Agents == nil {
		return nil, ErrUnavailable
	}
	if strings.TrimSpace(input.UserID) == "" || input.Limit <= 0 || input.Offset < 0 {
		return nil, ErrValidation
	}
	profiles, err := s.repos.Agents.ListProfilesForUser(ctx, input.UserID, input.Limit, input.Offset)
	if err != nil {
		return nil, mapRepositoryError(err)
	}
	return profiles, nil
}

func (s *Service) GetProfile(ctx context.Context, input GetProfileInput) (repository.AgentProfile, error) {
	if s == nil || s.repos.Agents == nil {
		return repository.AgentProfile{}, ErrUnavailable
	}
	if strings.TrimSpace(input.UserID) == "" || strings.TrimSpace(input.AgentID) == "" {
		return repository.AgentProfile{}, ErrValidation
	}
	profile, err := s.repos.Agents.GetProfileForUser(ctx, input.UserID, input.AgentID)
	if err != nil {
		return repository.AgentProfile{}, mapRepositoryError(err)
	}
	return profile, nil
}

func (s *Service) DeleteProfile(ctx context.Context, input DeleteProfileInput) error {
	if s == nil || s.repos.Agents == nil {
		return ErrUnavailable
	}
	if strings.TrimSpace(input.UserID) == "" || strings.TrimSpace(input.AgentID) == "" {
		return ErrValidation
	}
	return mapRepositoryError(s.repos.Agents.SoftDeleteProfile(ctx, input.UserID, input.AgentID))
}

func (s *Service) SyncDefaultMCPManifests(ctx context.Context) error {
	if s == nil || s.repos.MCP == nil {
		return ErrUnavailable
	}
	registry := mcp.NewManifestRegistry(
		mcp_websearch.Manifest(),
		mcp_knowledge.Manifest(),
		mcp_utility.Manifest(),
	)
	for _, manifest := range registry.List() {
		manifest = s.applyConfiguredManifestStatus(manifest)
		args, err := json.Marshal(manifest.Args)
		if err != nil {
			return err
		}
		env, err := json.Marshal(manifest.Env)
		if err != nil {
			return err
		}
		endpoint := normalizeOptionalString(&manifest.Endpoint)
		command := normalizeOptionalString(&manifest.Command)
		server, err := s.repos.MCP.UpsertServer(ctx, repository.UpsertMCPServerParams{
			Name:        manifest.Name,
			DisplayName: manifest.DisplayName,
			Transport:   string(manifest.Transport),
			Endpoint:    endpoint,
			Command:     command,
			Args:        args,
			Env:         env,
			SecretRefs:  json.RawMessage(`{}`),
			Status:      manifest.Status,
		})
		if err != nil {
			return mapRepositoryError(err)
		}
		for _, tool := range manifest.Tools {
			_, err := s.repos.MCP.UpsertTool(ctx, repository.UpsertMCPToolParams{
				ServerID:             server.ID,
				Name:                 tool.Name,
				NamespacedName:       "mcp:" + manifest.Name + "." + tool.Name,
				Description:          tool.Description,
				InputSchema:          tool.InputSchema,
				OutputSchema:         tool.OutputSchema,
				SideEffect:           tool.SideEffect,
				RequiresConfirmation: tool.RequiresConfirmation,
				Status:               tool.Status,
			})
			if err != nil {
				return mapRepositoryError(err)
			}
		}
	}
	return nil
}

func (s *Service) applyConfiguredManifestStatus(manifest mcp.ServerManifest) mcp.ServerManifest {
	if manifest.Name == mcp_websearch.ServerName &&
		(strings.TrimSpace(s.aiConfig.TavilyAPIKey) != "" || strings.TrimSpace(s.aiConfig.BraveSearchAPIKey) != "") {
		manifest.Status = "active"
	}
	return manifest
}

func mapRepositoryError(err error) error {
	if err == nil {
		return nil
	}
	return err
}

func normalizeOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	normalized := strings.TrimSpace(*value)
	if normalized == "" {
		return nil
	}
	return &normalized
}

func jsonObjectOrDefault(value json.RawMessage) json.RawMessage {
	if len(value) == 0 {
		return json.RawMessage(`{}`)
	}
	return value
}

func validJSONObject(value json.RawMessage) bool {
	if len(value) == 0 {
		return true
	}
	var decoded map[string]any
	return json.Unmarshal(value, &decoded) == nil
}

func normalizedOptional(value *string) *string {
	if value == nil {
		return nil
	}
	normalized := strings.TrimSpace(*value)
	if normalized == "" {
		return nil
	}
	return &normalized
}

func validateAgentAvatarFile(ctx context.Context, repos repository.Repositories, fileID string) error {
	if repos.Files == nil {
		return ErrUnavailable
	}
	file, err := repos.Files.Get(ctx, fileID)
	if err != nil {
		return err
	}
	if file.StorageStatus != "ready" {
		return ErrValidation
	}
	isAvatar, err := repos.Files.IsAvatarFile(ctx, file.ID)
	if err != nil {
		return err
	}
	if !isAvatar {
		return ErrValidation
	}
	return nil
}

package model

import (
	"errors"
	"strings"

	"nlserver/internal/config"
)

var (
	ErrModelProviderUnavailable = errors.New("agent: model provider unavailable")
	ErrModelProviderUnsupported = errors.New("agent: model provider unsupported")
)

type ModelProvider interface {
	Name() string
	Supports(provider string) bool
	NewGateway(cfg config.AIConfig) (ModelGateway, error)
}

type ModelProviderRegistry struct {
	providers []ModelProvider
}

func NewModelProviderRegistry(providers ...ModelProvider) *ModelProviderRegistry {
	registry := &ModelProviderRegistry{}
	for _, provider := range providers {
		if provider == nil {
			continue
		}
		registry.providers = append(registry.providers, provider)
	}
	return registry
}

func DefaultModelProviderRegistry() *ModelProviderRegistry {
	return NewModelProviderRegistry(OpenAICompatibleModelProvider{})
}

func NewModelGateway(cfg config.AIConfig) (ModelGateway, error) {
	return DefaultModelProviderRegistry().NewGateway(cfg)
}

func (r *ModelProviderRegistry) NewGateway(cfg config.AIConfig) (ModelGateway, error) {
	providerName := normalizeModelProvider(cfg.Provider)
	if providerName == "" || providerName == "disabled" {
		return nil, ErrModelProviderUnavailable
	}
	if r == nil {
		return nil, ErrModelProviderUnsupported
	}
	for _, provider := range r.providers {
		if provider.Supports(providerName) {
			gateway, err := provider.NewGateway(cfg)
			if err != nil {
				return nil, err
			}
			return wrapLangSmithGateway(gateway, cfg), nil
		}
	}
	return nil, ErrModelProviderUnsupported
}

func normalizeModelProvider(provider string) string {
	return strings.ToLower(strings.TrimSpace(provider))
}

package guardrails

import (
	"net/url"
	"strings"
)

type ToolPolicy struct {
	AllowedTools      []string `json:"allowedTools,omitempty"`
	AllowedMCPServers []string `json:"allowedMcpServers,omitempty"`
	AllowedMCPTools   []string `json:"allowedMcpTools,omitempty"`
	AllowedDomains    []string `json:"allowedDomains,omitempty"`
}

type SafetyPolicy struct {
	RequireConfirmationForSideEffects bool `json:"requireConfirmationForSideEffects,omitempty"`
	AllowMCPTools                     bool `json:"allowMcpTools,omitempty"`
}

func ToolAllowed(policy ToolPolicy, toolName string) bool {
	if toolName == "" {
		return false
	}
	if len(policy.AllowedTools) == 0 {
		return false
	}
	for _, allowed := range policy.AllowedTools {
		if allowed == toolName {
			return true
		}
	}
	return false
}

func MCPServerAllowed(policy ToolPolicy, serverName string) bool {
	if serverName == "" || len(policy.AllowedMCPServers) == 0 {
		return false
	}
	for _, allowed := range policy.AllowedMCPServers {
		if allowed == serverName {
			return true
		}
	}
	return false
}

func MCPToolAllowed(policy ToolPolicy, namespacedToolName string) bool {
	if namespacedToolName == "" || len(policy.AllowedMCPTools) == 0 {
		return false
	}
	for _, allowed := range policy.AllowedMCPTools {
		if allowed == namespacedToolName {
			return true
		}
	}
	return false
}

func DomainAllowed(policy ToolPolicy, domain string) bool {
	domain = NormalizeDomain(domain)
	if domain == "" {
		return false
	}
	allowedDomains := NormalizeDomains(policy.AllowedDomains)
	if len(allowedDomains) == 0 {
		return true
	}
	for _, allowed := range allowedDomains {
		if domain == allowed || strings.HasSuffix(domain, "."+allowed) {
			return true
		}
	}
	return false
}

func NormalizeDomains(domains []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, domain := range domains {
		normalized := NormalizeDomain(domain)
		if normalized == "" || seen[normalized] {
			continue
		}
		seen[normalized] = true
		result = append(result, normalized)
	}
	return result
}

func NormalizeDomain(domain string) string {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return ""
	}
	if parsed, err := url.Parse(domain); err == nil && parsed.Hostname() != "" {
		domain = parsed.Hostname()
	}
	domain = strings.TrimPrefix(domain, "www.")
	domain = strings.Trim(domain, ".")
	if domain == "" || strings.ContainsAny(domain, "/ :") {
		return ""
	}
	return domain
}

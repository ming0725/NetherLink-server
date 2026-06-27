package server

import (
	"errors"
	"strings"
	"testing"
)

func TestAIProviderNetworkErrorMapsToProviderError(t *testing.T) {
	err := errors.New(`Post "https://api.deepseek.com/chat/completions": dial tcp: lookup api.deepseek.com: i/o timeout`)

	code, message := aiErrorCodeAndMessage(err)
	if code != "AI_PROVIDER_ERROR" {
		t.Fatalf("code = %q, want AI_PROVIDER_ERROR", code)
	}
	if !strings.Contains(message, "lookup api.deepseek.com") {
		t.Fatalf("message = %q, want provider DNS detail", message)
	}
}

func TestAIInternalErrorClassificationForLogs(t *testing.T) {
	err := errors.New(`Post "https://api.deepseek.com/chat/completions": dial tcp: lookup api.deepseek.com: i/o timeout`)
	if got := aiInternalErrorKind(err); got != "timeout" {
		t.Fatalf("error kind = %q, want timeout", got)
	}
	if got := aiInternalErrorProvider(err); got != "deepseek" {
		t.Fatalf("provider = %q, want deepseek", got)
	}
	if got := aiInternalErrorCause(err); !strings.Contains(got, "lookup api.deepseek.com") {
		t.Fatalf("cause = %q, want DNS detail", got)
	}
}

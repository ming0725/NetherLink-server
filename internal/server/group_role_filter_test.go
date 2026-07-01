package server

import "testing"

func TestValidateOptionalGroupRoleFilterAcceptsAIRole(t *testing.T) {
	if err := validateOptionalGroupRoleFilter("ai"); err != nil {
		t.Fatalf("validateOptionalGroupRoleFilter(ai) = %v, want nil", err)
	}
	if err := validateOptionalGroupRoleFilter("bot"); err == nil {
		t.Fatal("validateOptionalGroupRoleFilter(bot) = nil, want validation error")
	}
}

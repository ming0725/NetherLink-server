package tests

import (
	"os"
	"strings"
	"testing"
)

func TestRealtimeEventContractNames(t *testing.T) {
	files := []string{
		"../internal/server/auth_handlers.go",
		"../internal/server/file_handlers.go",
		"../internal/server/notification_handlers.go",
		"../internal/server/realtime_event_helpers.go",
	}
	combined := ""
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		combined += string(data)
	}

	required := []string{
		`"profile.updated"`,
		`"preference.updated"`,
		`"notification.read.updated"`,
	}
	for _, eventName := range required {
		if !strings.Contains(combined, eventName) {
			t.Fatalf("missing realtime event name %s", eventName)
		}
	}

	forbidden := []string{
		`"user.profile.updated"`,
		`"user.preferences.updated"`,
		`"notification.read_all"`,
		`"notification.read"`,
	}
	for _, eventName := range forbidden {
		if strings.Contains(combined, eventName) {
			t.Fatalf("legacy realtime event name still present: %s", eventName)
		}
	}
}

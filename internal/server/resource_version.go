package server

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

var errResourceVersionConflict = errors.New("resource version conflict")

func resourceETag(resourceType string, resourceID string, version int) string {
	return fmt.Sprintf(`"%s-%s-v%d"`, resourceType, resourceID, version)
}

func writeJSONWithETag(w http.ResponseWriter, r *http.Request, status int, etag string, payload any) {
	if etag != "" {
		w.Header().Set("ETag", etag)
	}
	if r.Method == http.MethodGet && etag != "" && ifNoneMatch(r, etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	writeJSON(w, status, payload)
}

func ifNoneMatch(r *http.Request, etag string) bool {
	return etagListMatches(r.Header.Get("If-None-Match"), etag)
}

func checkResourceVersion(w http.ResponseWriter, r *http.Request, resourceETag string, currentVersion int, expectedVersion *int) bool {
	if !resourceVersionMatches(r, resourceETag, currentVersion, expectedVersion) {
		writeError(w, r, http.StatusConflict, "VERSION_CONFLICT", "Resource version conflict")
		return false
	}
	return true
}

func resourceVersionMatches(r *http.Request, resourceETag string, currentVersion int, expectedVersion *int) bool {
	if expectedVersion != nil && *expectedVersion != currentVersion {
		return false
	}
	if header := strings.TrimSpace(r.Header.Get("If-Match")); header != "" && !etagListMatches(header, resourceETag) {
		return false
	}
	return true
}

func etagListMatches(header string, etag string) bool {
	header = strings.TrimSpace(header)
	if header == "" || etag == "" {
		return false
	}
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || candidate == etag || candidate == strings.Trim(etag, `"`) {
			return true
		}
	}
	return false
}

func expectedVersionFromQuery(r *http.Request) (*int, *requestError) {
	value := strings.TrimSpace(r.URL.Query().Get("expectedVersion"))
	if value == "" {
		return nil, nil
	}
	version, err := strconv.Atoi(value)
	if err != nil || version < 1 {
		return nil, &requestError{
			Status:  http.StatusBadRequest,
			Code:    "VALIDATION_ERROR",
			Message: "expectedVersion must be a positive integer",
		}
	}
	return &version, nil
}

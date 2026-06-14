package server

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

const (
	defaultListLimit = 20
	maxListLimit     = 100
)

var (
	uuidPattern          = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	groupPublicIDPattern = regexp.MustCompile(`^[0-9]{10}$`)
)

func parsePagination(r *http.Request) (int, int, *requestError) {
	limit, err := parseOptionalNonNegativeInt(r.URL.Query().Get("limit"), defaultListLimit, "limit")
	if err != nil {
		return 0, 0, err
	}
	if limit == 0 {
		limit = defaultListLimit
	}
	if limit > maxListLimit {
		limit = maxListLimit
	}

	offset, err := parseOptionalNonNegativeInt(r.URL.Query().Get("offset"), 0, "offset")
	if err != nil {
		return 0, 0, err
	}

	return limit, offset, nil
}

func validateUUID(value string, field string) *requestError {
	if uuidPattern.MatchString(strings.TrimSpace(value)) {
		return nil
	}

	return &requestError{
		Status:  http.StatusBadRequest,
		Code:    "VALIDATION_ERROR",
		Message: fmt.Sprintf("%s must be a UUID", field),
	}
}

func validateOptionalUUID(value string, field string) *requestError {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return validateUUID(value, field)
}

func validateGroupPublicID(value string, field string) *requestError {
	if groupPublicIDPattern.MatchString(strings.TrimSpace(value)) {
		return nil
	}

	return &requestError{
		Status:  http.StatusBadRequest,
		Code:    "VALIDATION_ERROR",
		Message: fmt.Sprintf("%s must be a 10-digit number", field),
	}
}

func parseOptionalNonNegativeInt(value string, fallback int, field string) (int, *requestError) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback, nil
	}

	parsed, err := strconv.Atoi(trimmed)
	if err != nil || parsed < 0 {
		return 0, &requestError{
			Status:  http.StatusBadRequest,
			Code:    "VALIDATION_ERROR",
			Message: fmt.Sprintf("%s must be a non-negative integer", field),
		}
	}

	return parsed, nil
}

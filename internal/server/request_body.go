package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const maxJSONBodyBytes int64 = 1 << 20

type requestError struct {
	Status  int
	Code    string
	Message string
}

func (e *requestError) Error() string {
	return e.Code + ": " + e.Message
}

func decodeJSONBody(r *http.Request, dst any) *requestError {
	r.Body = http.MaxBytesReader(nil, r.Body, maxJSONBodyBytes)

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(dst); err != nil {
		return jsonDecodeError(err)
	}

	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return &requestError{
			Status:  http.StatusBadRequest,
			Code:    "INVALID_JSON",
			Message: "Request body must contain a single JSON object",
		}
	}

	return nil
}

func jsonDecodeError(err error) *requestError {
	var syntaxErr *json.SyntaxError
	var typeErr *json.UnmarshalTypeError

	switch {
	case errors.Is(err, io.EOF):
		return &requestError{
			Status:  http.StatusBadRequest,
			Code:    "INVALID_JSON",
			Message: "Request body is required",
		}
	case errors.As(err, &syntaxErr):
		return &requestError{
			Status:  http.StatusBadRequest,
			Code:    "INVALID_JSON",
			Message: fmt.Sprintf("Malformed JSON at byte offset %d", syntaxErr.Offset),
		}
	case errors.As(err, &typeErr):
		return &requestError{
			Status:  http.StatusBadRequest,
			Code:    "INVALID_JSON",
			Message: fmt.Sprintf("Invalid JSON value for field %q", typeErr.Field),
		}
	case strings.HasPrefix(err.Error(), "json: unknown field "):
		return &requestError{
			Status:  http.StatusBadRequest,
			Code:    "INVALID_FIELD",
			Message: strings.TrimPrefix(err.Error(), "json: "),
		}
	case strings.Contains(err.Error(), "request body too large"):
		return &requestError{
			Status:  http.StatusRequestEntityTooLarge,
			Code:    "REQUEST_TOO_LARGE",
			Message: "Request body is too large",
		}
	default:
		return &requestError{
			Status:  http.StatusBadRequest,
			Code:    "INVALID_JSON",
			Message: "Invalid request body",
		}
	}
}

func requireString(value string, field string) *requestError {
	if strings.TrimSpace(value) != "" {
		return nil
	}

	return &requestError{
		Status:  http.StatusBadRequest,
		Code:    "VALIDATION_ERROR",
		Message: fmt.Sprintf("%s is required", field),
	}
}

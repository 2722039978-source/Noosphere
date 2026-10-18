package errors

import (
    "fmt"
    "log"
	"net/http"
)

// Code is a machine-readable error code.
type Code string

const (
	ErrNotFound      Code = "NOT_FOUND"
	ErrInvalidInput  Code = "INVALID_INPUT"
	ErrUnauthorized  Code = "UNAUTHORIZED"
	ErrInternal      Code = "INTERNAL"
	ErrUnavailable   Code = "UNAVAILABLE"
	ErrTimeout       Code = "TIMEOUT"
	ErrConflict      Code = "CONFLICT"
)

// Category identifies which subsystem produced the error.
type Category string

const (
	CatGateway  Category = "gateway"
	CatNebula   Category = "nebula"
	CatDevOps   Category = "devops"
	CatCodeLens Category = "codelens"
	CatSystem   Category = "system"
)

// AppError is the unified application error type.
type AppError struct {
	Code       Code     `json:"code"`
	Category   Category `json:"category"`
	Message    string   `json:"message"`
	Cause      error    `json:"-"`
	HTTPStatus int      `json:"-"`
}

func (e *AppError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s/%s] %s: %v", e.Category, e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s/%s] %s", e.Category, e.Code, e.Message)
}

func (e *AppError) Unwrap() error { return e.Cause }

// New creates a new AppError with the given code, category, and message.
func New(code Code, category Category, msg string) *AppError {
	return &AppError{
		Code:       code,
		Category:   category,
		Message:    msg,
		HTTPStatus: codeToHTTP(code),
	}
}

// Wrap wraps an existing error into an AppError.
func Wrap(err error, code Code, category Category, msg string) *AppError {
	return &AppError{
		Code:       code,
		Category:   category,
		Message:    msg,
		Cause:      err,
		HTTPStatus: codeToHTTP(code),
	}
}

// NotFound creates a NOT_FOUND error for the given category.
func NotFound(category Category, msg string) *AppError {
	return New(ErrNotFound, category, msg)
}

// InvalidInput creates an INVALID_INPUT error for the given category.
func InvalidInput(category Category, msg string) *AppError {
	return New(ErrInvalidInput, category, msg)
}

// Internal creates an INTERNAL error for the given category.
func Internal(category Category, msg string) *AppError {
	return New(ErrInternal, category, msg)
}

func codeToHTTP(c Code) int {
	switch c {
	case ErrNotFound:
		return http.StatusNotFound
	case ErrInvalidInput:
		return http.StatusBadRequest
	case ErrUnauthorized:
		return http.StatusUnauthorized
	case ErrConflict:
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

// WriteJSON writes an AppError as a JSON response.
func WriteJSON(w http.ResponseWriter, err *AppError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(err.HTTPStatus)
	fmt.Fprintf(w, `{"error":{"code":"%s","category":"%s","message":"%s"}}`,
		err.Code, err.Category, err.Message)
}

// WriteHTTP writes an AppError as a structured JSON HTTP response.
func WriteHTTP(w http.ResponseWriter, r *http.Request, err *AppError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(err.HTTPStatus)
	fmt.Fprintf(w, `{"error":{"code":"%s","category":"%s","message":"%s"}}`,
		err.Code, err.Category, err.Message)
}

// HandlerFunc is an HTTP handler function that returns an AppError.
type HandlerFunc func(w http.ResponseWriter, r *http.Request) *AppError

// WrapHandler converts an error-returning handler to standard http.HandlerFunc.
func WrapHandler(fn HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := fn(w, r); err != nil {
			log.Printf("[error] %s %s -> [%s/%s] %s",
				r.Method, r.URL.Path, err.Category, err.Code, err.Message)
			WriteHTTP(w, r, err)
		}
	}
}

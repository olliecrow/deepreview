package deepreview

import (
	"errors"
	"fmt"
)

var ErrDeepReview = errors.New("deepreview error")

type DeepReviewError struct {
	Message string
}

func (e *DeepReviewError) Error() string {
	return e.Message
}

func (e *DeepReviewError) Unwrap() error {
	return ErrDeepReview
}

func NewDeepReviewError(format string, args ...any) error {
	return &DeepReviewError{Message: fmt.Sprintf(format, args...)}
}

type CommandExecutionError struct {
	Message  string
	Command  []string
	Code     int
	Stdout   string
	Stderr   string
	TimedOut bool
}

func (e *CommandExecutionError) Error() string {
	return e.Message
}

func (e *CommandExecutionError) Unwrap() error {
	return ErrDeepReview
}

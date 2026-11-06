package deepreview

import (
	"errors"
	"strings"
)

const (
	outcomeComplete   = "complete"
	outcomeNoChange   = "no-change"
	outcomeIncomplete = "incomplete"
	outcomeFailed     = "failed"
	outcomeCanceled   = "canceled"

	publicationNotAttempted = "not-attempted"
	publicationPushed       = "pushed"
	publicationPRCreated    = "pr-created"

	exitCodeIncomplete = 2
)

type terminalOutcome string

type incompleteRunError struct {
	cause error
}

func (e *incompleteRunError) Error() string {
	if e == nil || e.cause == nil {
		return "run ended incomplete"
	}
	return e.cause.Error()
}

func (e *incompleteRunError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func newIncompleteRunError(cause error) error {
	if cause == nil {
		cause = NewDeepReviewError("run ended incomplete")
	}
	var already *incompleteRunError
	if errors.As(cause, &already) {
		return cause
	}
	return &incompleteRunError{cause: cause}
}

func isIncompleteRunError(err error) bool {
	var target *incompleteRunError
	return errors.As(err, &target)
}

func reviewExitCode(err error, interrupted bool) int {
	if interrupted || isInterruptError(err) {
		return 130
	}
	if isIncompleteRunError(err) {
		return exitCodeIncomplete
	}
	if err != nil {
		return 1
	}
	return 0
}

func (d DeliveryResult) publicationState() string {
	switch d.Publication {
	case publicationNotAttempted, publicationPushed, publicationPRCreated:
		return d.Publication
	}
	if strings.TrimSpace(d.PRURL) != "" {
		return publicationPRCreated
	}
	if strings.TrimSpace(d.PushedRefspec) != "" || strings.TrimSpace(d.CommitsURL) != "" {
		return publicationPushed
	}
	return publicationNotAttempted
}

func (d DeliveryResult) showPRURL() bool {
	return d.publicationState() == publicationPRCreated && strings.TrimSpace(d.PRURL) != ""
}

func (d DeliveryResult) showCommitsURL() bool {
	switch d.publicationState() {
	case publicationPushed, publicationPRCreated:
		return strings.TrimSpace(d.CommitsURL) != ""
	default:
		return false
	}
}

func (o *Orchestrator) hasPublishedDelivery() bool {
	if o == nil || o.lastDelivery == nil {
		return false
	}
	switch o.lastDelivery.publicationState() {
	case publicationPushed, publicationPRCreated:
		return true
	default:
		return false
	}
}

func (o *Orchestrator) noteSummaryDiagnostic(err error) {
	if o == nil || err == nil {
		return
	}
	if o.summaryDiagnostic == nil {
		o.summaryDiagnostic = err
		return
	}
	o.summaryDiagnostic = errors.Join(o.summaryDiagnostic, err)
}

func (o *Orchestrator) SummaryDiagnostic() error {
	if o == nil {
		return nil
	}
	return o.summaryDiagnostic
}

func (o *Orchestrator) Outcome() string {
	if o == nil {
		return ""
	}
	return string(o.outcome)
}

// noteObservationalSummaryError keeps a published or incomplete result when
// only the operator summary failed. A run that has not published still fails.
func (o *Orchestrator) noteObservationalSummaryError(err error) error {
	if err == nil {
		return nil
	}
	incomplete := o != nil && o.lastDelivery != nil && o.lastDelivery.Incomplete
	if o.hasPublishedDelivery() || incomplete {
		o.noteSummaryDiagnostic(err)
		return nil
	}
	return err
}

func (o *Orchestrator) acceptDeliverySummary(delivery DeliveryResult, summaries []string) error {
	o.lastDelivery = &delivery
	if err := o.writeFinalSummary(delivery, summaries); err != nil {
		if kept := o.noteObservationalSummaryError(err); kept != nil {
			return kept
		}
	}
	if delivery.Incomplete {
		reason := strings.TrimSpace(delivery.IncompleteReason)
		if reason == "" {
			reason = "delivery did not finish"
		}
		return newIncompleteRunError(NewDeepReviewError("%s", reason))
	}
	return nil
}

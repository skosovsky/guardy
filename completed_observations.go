package guardy

// CompletedObservationsError carries explicitly attested observations completed
// before a processing error. The failed callback's report/output are not evidence.
// Fields are private; construction and projections snapshot reports without MutatedText.
// The host trusts adapter code to attest only completed checks, as with capabilities.
type CompletedObservationsError struct {
	cause     error
	completed *Report
}

// Error exposes a fixed fault category, never the diagnostic cause or observations.
func (*CompletedObservationsError) Error() string { return ErrValidatorFailed.Error() }

// Unwrap preserves original typed errors and wrapped cancellation.
func (e *CompletedObservationsError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// CompletedReport returns an independent snapshot, never a transformed value.
func (e *CompletedObservationsError) CompletedReport() *Report {
	if e == nil {
		return nil
	}
	return e.completed.CloneWithoutState()
}

// WithCompletedObservations attaches only previously completed reports to a fault.
// It combines nested carrier evidence from wrapped/joined causes. A nil cause
// returns nil; with no observations the original error is returned unchanged.
// Reports returned by the failing callback must not be passed as completed.
func WithCompletedObservations(cause error, completed ...*Report) error {
	if cause == nil {
		return nil
	}
	reports := append([]*Report(nil), completed...)
	reports = append(reports, CompletedReportFromError(cause))
	var present bool
	for _, rep := range reports {
		present = present || rep != nil
	}
	if !present {
		return cause
	}
	return &CompletedObservationsError{cause: cause, completed: ComposeReports(reports...).CloneWithoutState()}
}

// CompletedReportFromError projects explicitly attested completed observations
// through ordinary wrappers and all joined branches. A raw failed report is never
// consulted. A carrier already includes its nested evidence, so its cause is not
// walked twice. The result is a fresh snapshot; nil means no attested observation.
func CompletedReportFromError(cause error) *Report {
	var reports []*Report
	collectCompletedReports(cause, &reports)
	if len(reports) == 0 {
		return nil
	}
	return ComposeReports(reports...).CloneWithoutState()
}

func collectCompletedReports(cause error, reports *[]*Report) {
	//nolint:errorlint // Inspect this node only; errors.As would skip sibling joined branches.
	switch current := cause.(type) {
	case *CompletedObservationsError:
		if rep := current.CompletedReport(); rep != nil {
			*reports = append(*reports, rep)
		}
	case interface{ Unwrap() []error }:
		for _, child := range current.Unwrap() {
			collectCompletedReports(child, reports)
		}
	case interface{ Unwrap() error }:
		collectCompletedReports(current.Unwrap(), reports)
	}
}

func appendCompletedObservations(reports []Report, cause error) []Report {
	if rep := CompletedReportFromError(cause); rep != nil {
		return append(reports, normalizeReport(rep))
	}
	return reports
}

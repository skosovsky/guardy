package guardy

import (
	"context"
	"errors"
	"testing"
)

type bindUser struct {
	Name string `json:"name"`
}

func TestValidateAndDecode_Success(t *testing.T) {
	t.Parallel()
	p := NewPipeline(WithFastPath(ValidatorFunc[string](func(_ context.Context, input string) (string, *Report, error) {
		return input, &Report{Action: ActionPass, Validator: "pass"}, nil
	})))
	user, rep, err := ValidateAndDecode[bindUser](context.Background(), nil, p, `{"name":"Ada"}`)
	if err != nil {
		t.Fatal(err)
	}
	if user == nil || user.Name != "Ada" {
		t.Fatalf("user = %+v", user)
	}
	if rep == nil || rep.Action != ActionPass {
		t.Fatalf("rep = %+v", rep)
	}
}

func TestValidateAndDecode_Block(t *testing.T) {
	t.Parallel()
	p := NewPipeline(WithFastPath(ValidatorFunc[string](func(_ context.Context, input string) (string, *Report, error) {
		return input, &Report{Action: ActionBlock, Validator: "b", Reason: "nope"}, nil
	})))
	_, rep, err := ValidateAndDecode[bindUser](context.Background(), nil, p, `{}`)
	var blockErr *BlockError
	if !errors.As(err, &blockErr) {
		t.Fatalf("expected BlockError, got %v", err)
	}
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("err = %v", err)
	}
	if rep == nil || rep.Action != ActionBlock {
		t.Fatalf("rep = %+v", rep)
	}
	if blockErr.Report.Disposition != DispositionTerminalDeny {
		t.Fatalf("disposition = %v", blockErr.Report.Disposition)
	}
}

func TestValidateAndDecode_FatalPolicyStop(t *testing.T) {
	t.Parallel()
	p := NewPipeline(
		WithPolicyValidators(NewAttributeEquals[string]("resource.id", "expected", WithPolicyFatal(true))),
	)
	_, rep, err := ValidateAndDecode[bindUser](
		context.Background(),
		MapScope{"resource.id": "wrong"},
		p,
		`{"name":"Ada"}`,
	)
	var blockErr *BlockError
	if !errors.As(err, &blockErr) {
		t.Fatalf("expected BlockError, got %v", err)
	}
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("err = %v", err)
	}
	if rep == nil || !rep.Fatal {
		t.Fatalf("rep = %+v", rep)
	}
	if blockErr.Report.Disposition != DispositionTerminalDeny {
		t.Fatalf("disposition = %v", blockErr.Report.Disposition)
	}
}

func TestValidateAndDecode_TerminalRetryReturnsBlockError(t *testing.T) {
	t.Parallel()
	p := NewPipeline(WithFastPath(ValidatorFunc[string](func(_ context.Context, input string) (string, *Report, error) {
		return input, FinishReport(&Report{
			Action:    ActionRetry,
			Retryable: false,
			Reason:    "terminal",
		}, ControlSpec{Action: ActionRetry, Retryable: new(false)}), nil
	})))
	_, _, err := ValidateAndDecode[bindUser](context.Background(), nil, p, `{"name":"Ada"}`)
	var blockErr *BlockError
	if !errors.As(err, &blockErr) {
		t.Fatalf("expected BlockError, got %v", err)
	}
	if blockErr.Report.Disposition != DispositionTerminalDeny {
		t.Fatalf("disposition = %v", blockErr.Report.Disposition)
	}
}

func TestValidateAndDecode_InvalidJSONAfterPipeline(t *testing.T) {
	t.Parallel()
	p := NewPipeline(WithFastPath(ValidatorFunc[string](func(_ context.Context, input string) (string, *Report, error) {
		return input, &Report{Action: ActionPass, Validator: "pass"}, nil
	})))
	_, rep, err := ValidateAndDecode[bindUser](context.Background(), nil, p, `not-json`)
	var retry *RetryError
	if !errors.As(err, &retry) {
		t.Fatalf("expected RetryError, got %v", err)
	}
	if rep == nil || rep.Code != CodeJSONInvalid {
		t.Fatalf("rep = %+v", rep)
	}
	if rep.Disposition != DispositionRetryableCorrection {
		t.Fatalf("disposition = %v", rep.Disposition)
	}
}

type bindAgeCheck struct {
	Age int `json:"age"`
}

func (b *bindAgeCheck) ValidatePostBind(context.Context) error {
	if b.Age < 18 {
		return errors.New("age must be 18+")
	}
	return nil
}

func TestValidateAndDecode_PostBindViolation(t *testing.T) {
	t.Parallel()
	p := NewPipeline(WithFastPath(ValidatorFunc[string](func(_ context.Context, input string) (string, *Report, error) {
		return input, &Report{Action: ActionPass, Validator: "pass"}, nil
	})))
	_, rep, err := ValidateAndDecode[bindAgeCheck](context.Background(), nil, p, `{"age":10}`)
	var retry *RetryError
	if !errors.As(err, &retry) {
		t.Fatalf("expected RetryError, got %v", err)
	}
	if rep == nil || rep.Code != CodePostBindViolation {
		t.Fatalf("rep = %+v", rep)
	}
}

func TestValidateAndDecode_ScopeIncomplete(t *testing.T) {
	t.Parallel()
	p := NewPipeline(
		WithPolicyValidators(NewAttributePresent[string]("resource.id")),
	)
	_, _, err := ValidateAndDecode[bindUser](context.Background(), MapScope{}, p, `{"name":"Ada"}`)
	if !errors.Is(err, ErrScopeIncomplete) {
		t.Fatalf("err = %v", err)
	}
}

func TestValidateAndDecode_UsesRedactedOutput(t *testing.T) {
	t.Parallel()
	p := NewPipeline(WithFastPath(ValidatorFunc[string](func(_ context.Context, _ string) (string, *Report, error) {
		return `{"name":"Redacted"}`, &Report{
			Action: ActionRedact, Validator: "redact", MutatedText: `{"name":"Redacted"}`,
		}, nil
	})))
	user, _, err := ValidateAndDecode[bindUser](context.Background(), nil, p, `{"name":"Secret"}`)
	if err != nil {
		t.Fatal(err)
	}
	if user == nil || user.Name != "Redacted" {
		t.Fatalf("user = %+v", user)
	}
}

func TestValidateAndDecode_UserChannelBlocksTechnicalPayload(t *testing.T) {
	t.Parallel()
	p := NewPipeline(
		WithUserChannel[string](),
		WithUserChannelFallback[string]("blocked for user"),
		WithFastPath(ValidatorFunc[string](func(_ context.Context, input string) (string, *Report, error) {
			return input, FinishReport(&Report{
				Action: ActionPass, Validator: "classifier", PayloadKind: PayloadTechnicalPayload,
			}, ControlSpec{Action: ActionPass}), nil
		})),
	)
	_, rep, err := ValidateAndDecode[bindUser](context.Background(), nil, p, `{"tool":"x"}`)
	var blockErr *BlockError
	if !errors.As(err, &blockErr) {
		t.Fatalf("expected BlockError, got %v", err)
	}
	if rep == nil || rep.PayloadKind != PayloadTechnicalPayload {
		t.Fatalf("PayloadKind = %v", rep)
	}
	if blockErr.Report.PublicMessage() != "blocked for user" {
		t.Fatalf("PublicMessage = %q", blockErr.Report.PublicMessage())
	}
}

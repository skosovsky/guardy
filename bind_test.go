package guardy

import (
	"context"
	"errors"
	"testing"
)

type bindUser struct {
	Name string `json:"name"`
}

func TestValidateAndBind_Success(t *testing.T) {
	t.Parallel()
	p := NewPipeline(WithFastPath(ValidatorFunc[string](func(_ context.Context, input string) (string, *Report, error) {
		return input, &Report{Action: ActionPass, Validator: "pass"}, nil
	})))
	user, rep, err := ValidateAndBind[bindUser](context.Background(), p, `{"name":"Ada"}`)
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

func TestValidateAndBind_Block(t *testing.T) {
	t.Parallel()
	p := NewPipeline(WithFastPath(ValidatorFunc[string](func(_ context.Context, input string) (string, *Report, error) {
		return input, &Report{Action: ActionBlock, Validator: "b", Reason: "nope"}, nil
	})))
	_, rep, err := ValidateAndBind[bindUser](context.Background(), p, `{}`)
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("err = %v", err)
	}
	if rep == nil || rep.Action != ActionBlock {
		t.Fatalf("rep = %+v", rep)
	}
}

func TestValidateAndBind_FatalPolicyStop(t *testing.T) {
	t.Parallel()
	p := NewPipeline(
		WithPolicyValidators(NewAttributePresent[string]("resource.id", WithPolicyFatal(true))),
	)
	ctx := WithAttributes(context.Background(), Attributes{})
	_, rep, err := ValidateAndBind[bindUser](ctx, p, `{"name":"Ada"}`)
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("err = %v", err)
	}
	if rep == nil || !rep.Fatal {
		t.Fatalf("rep = %+v", rep)
	}
}

func TestValidateAndBind_InvalidJSONAfterPipeline(t *testing.T) {
	t.Parallel()
	p := NewPipeline(WithFastPath(ValidatorFunc[string](func(_ context.Context, input string) (string, *Report, error) {
		return input, &Report{Action: ActionPass, Validator: "pass"}, nil
	})))
	_, rep, err := ValidateAndBind[bindUser](context.Background(), p, `not-json`)
	var retry *RetryError
	if !errors.As(err, &retry) {
		t.Fatalf("expected RetryError, got %v", err)
	}
	if rep == nil || rep.Code != CodeJSONInvalid {
		t.Fatalf("rep = %+v", rep)
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

func TestValidateAndBind_PostBindValidator_Pass(t *testing.T) {
	t.Parallel()
	p := NewPipeline(WithFastPath(ValidatorFunc[string](func(_ context.Context, input string) (string, *Report, error) {
		return input, &Report{Action: ActionPass, Validator: "pass"}, nil
	})))
	user, rep, err := ValidateAndBind[bindAgeCheck](context.Background(), p, `{"age":21}`)
	if err != nil {
		t.Fatal(err)
	}
	if user == nil || user.Age != 21 {
		t.Fatalf("user = %+v", user)
	}
	if rep == nil || rep.Action != ActionPass {
		t.Fatalf("rep = %+v", rep)
	}
}

func TestValidateAndBind_PostBindValidator_Retry(t *testing.T) {
	t.Parallel()
	p := NewPipeline(WithFastPath(ValidatorFunc[string](func(_ context.Context, input string) (string, *Report, error) {
		return input, &Report{Action: ActionPass, Validator: "pass"}, nil
	})))
	_, rep, err := ValidateAndBind[bindAgeCheck](context.Background(), p, `{"age":10}`)
	var retry *RetryError
	if !errors.As(err, &retry) {
		t.Fatalf("expected RetryError, got %v", err)
	}
	if rep == nil || rep.Code != CodePostBindViolation {
		t.Fatalf("rep = %+v", rep)
	}
	if !rep.Retryable {
		t.Error("post-bind retry should be Retryable")
	}
}

func TestValidateAndBind_NoPostBind_Unchanged(t *testing.T) {
	t.Parallel()
	p := NewPipeline(WithFastPath(ValidatorFunc[string](func(_ context.Context, input string) (string, *Report, error) {
		return input, &Report{Action: ActionPass, Validator: "pass"}, nil
	})))
	user, _, err := ValidateAndBind[bindUser](context.Background(), p, `{"name":"Ada"}`)
	if err != nil {
		t.Fatal(err)
	}
	if user == nil || user.Name != "Ada" {
		t.Fatalf("user = %+v", user)
	}
}

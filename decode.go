package guardy

import (
	"context"
	"errors"
	"reflect"
)

// PostBindValidator is optional domain validation after JSON unmarshal in [ArgsPipeline.Validate].
// Domain errors request correction; cancellation/deadline errors are system faults.
// Implement on a pointer receiver, for example:
//
//	func (u *User) ValidatePostBind(ctx context.Context) error
type PostBindValidator interface {
	ValidatePostBind(ctx context.Context) error
}

func invokePostBind(ctx context.Context, v any) error {
	value := reflect.ValueOf(v)
	for value.IsValid() {
		if value.Kind() == reflect.Pointer && value.IsNil() {
			return errors.New("guardy: null argument cannot be validated")
		}
		if pb, ok := reflect.TypeAssert[PostBindValidator](value); ok {
			return pb.ValidatePostBind(ctx)
		}
		if value.Kind() != reflect.Pointer && value.Kind() != reflect.Interface {
			break
		}
		value = value.Elem()
	}
	return nil
}

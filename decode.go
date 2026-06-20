package guardy

import (
	"context"
)

// PostBindValidator is optional domain validation after JSON unmarshal in [ArgsPipeline.Validate].
// Implement on a pointer receiver, for example:
//
//	func (u *User) ValidatePostBind(ctx context.Context) error
type PostBindValidator interface {
	ValidatePostBind(ctx context.Context) error
}

func invokePostBind(ctx context.Context, v any) error {
	pb, ok := v.(PostBindValidator)
	if !ok {
		return nil
	}
	return pb.ValidatePostBind(ctx)
}

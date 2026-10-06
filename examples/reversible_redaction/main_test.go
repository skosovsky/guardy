package main

import (
	"context"
	"testing"

	"github.com/skosovsky/guardy"
	"github.com/skosovsky/guardy/ext"
)

type observedVault struct {
	vault    ext.TokenVault
	restores int
}

func (v *observedVault) Store(namespace, original string) (string, error) {
	return v.vault.Store(namespace, original)
}
func (v *observedVault) Restore(token string) (string, bool) {
	v.restores++
	return v.vault.Restore(token)
}

func TestRecipientAuthorizationPrecedesRestorationAndFinalDelivery(t *testing.T) {
	for _, tc := range []struct {
		name, recipient string
		denyFinal       bool
		wantRestores    int
		wantDelivery    bool
	}{
		{"unauthorized", "external", false, 0, false},
		{"authorized", "owner", false, 1, true},
		{"final deny", "owner", true, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange: host facts determine disclosure; token ownership is insufficient.
			vault := &observedVault{vault: ext.NewInMemoryTokenVault()}
			token, storeErr := vault.Store(ext.TokenNamespacePII, "user@example.com")
			if storeErr != nil {
				t.Fatal(storeErr)
			}
			final := guardy.NewPipeline[string]()
			if tc.denyFinal {
				final = guardy.NewPipeline(
					guardy.WithFastPath(ext.MustPIIValidator(ext.WithAction(guardy.ActionBlock))),
				)
			}
			// Act.
			result, err := restoreForRecipient(context.Background(), tc.recipient, "owner", token, vault, final)
			value, delivered := result.DeliverableValue()
			// Assert: only final-checked data may become a consumer value.
			if vault.restores != tc.wantRestores || delivered != tc.wantDelivery ||
				(tc.wantDelivery && (err != nil || value != "user@example.com")) ||
				(!tc.wantDelivery && err == nil) {
				t.Fatalf("restores=%d value=%q delivered=%v err=%v", vault.restores, value, delivered, err)
			}
		})
	}
}

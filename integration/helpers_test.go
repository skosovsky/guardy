package integration_test

import (
	"time"

	g "github.com/skosovsky/guardy"
)

func streamConfig(p *g.Pipeline[string]) g.StreamConfig {
	return g.StreamConfig{
		Identity:          "test",
		Profile:           g.ReleaseWholeResponse,
		Pipeline:          p,
		MaxInputBytes:     4096,
		MaxPendingBytes:   4096,
		MaxUnitBytes:      4096,
		MaxOutputBytes:    4096,
		ValidationTimeout: time.Second,
		Delivery:          g.NewDeliveryPolicy("external"),
	}
}

type documentClaims struct {
	ClaimedTrust string
	Content      string
}

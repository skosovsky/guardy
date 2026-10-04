module github.com/skosovsky/guardy/examples/otel_integration

go 1.27.1

require (
	github.com/skosovsky/guardy v0.11.1
	github.com/skosovsky/guardy/ext/guardyotel v0.11.1
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel v1.47.0 // indirect
	go.opentelemetry.io/otel/log v1.47.0 // indirect
	go.opentelemetry.io/otel/metric v1.47.0 // indirect
	go.opentelemetry.io/otel/trace v1.47.0 // indirect
	golang.org/x/sync v0.23.0 // indirect
)

replace github.com/skosovsky/guardy => ../..

replace github.com/skosovsky/guardy/ext/guardyotel => ../../ext/guardyotel

module github.com/skosovsky/guardy/examples/declarative_guard

go 1.27.1

require (
	github.com/skosovsky/guardy v0.11.1
	github.com/skosovsky/guardy/build v0.11.1
)

require (
	github.com/bahlo/generic-list-go v0.2.0 // indirect
	github.com/buger/jsonparser v1.6.1 // indirect
	github.com/invopop/jsonschema v0.14.0 // indirect
	github.com/pb33f/go-yaml v0.1.1 // indirect
	github.com/pb33f/ordered-map/v2 v2.3.2 // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.2 // indirect
	github.com/skosovsky/guardy/ext/jsonschema v0.11.1 // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/text v0.14.0 // indirect
)

replace (
	github.com/skosovsky/guardy => ../..
	github.com/skosovsky/guardy/build => ../../build
)

replace github.com/skosovsky/guardy/ext/jsonredact => ../../ext/jsonredact

replace github.com/skosovsky/guardy/ext/jsonschema => ../../ext/jsonschema

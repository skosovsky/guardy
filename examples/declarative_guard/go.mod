module github.com/skosovsky/guardy/examples/declarative_guard

go 1.26.1

require (
	github.com/skosovsky/guardy v0.0.0
	github.com/skosovsky/guardy/build v0.0.0
)

require golang.org/x/sync v0.20.0 // indirect

replace (
	github.com/skosovsky/guardy => ../..
	github.com/skosovsky/guardy/build => ../../build
)

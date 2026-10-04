module github.com/skosovsky/guardy/build

go 1.27.1

require (
	github.com/skosovsky/guardy v0.11.1
	github.com/skosovsky/guardy/ext/jsonschema v0.11.1
)

require (
	github.com/bahlo/generic-list-go v0.2.0 // indirect
	github.com/buger/jsonparser v1.6.1 // indirect
	github.com/invopop/jsonschema v0.14.0 // indirect
	github.com/pb33f/go-yaml v0.1.1 // indirect
	github.com/pb33f/ordered-map/v2 v2.3.2 // indirect
	github.com/xeipuuv/gojsonpointer v0.0.0-20190905194746-02993c407bfb // indirect
	github.com/xeipuuv/gojsonreference v0.0.0-20180127040603-bd5ef7bd5415 // indirect
	github.com/xeipuuv/gojsonschema v1.2.0 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/sync v0.23.0 // indirect
)

replace (
	github.com/skosovsky/guardy => ..
	github.com/skosovsky/guardy/ext/jsonschema => ../ext/jsonschema
)

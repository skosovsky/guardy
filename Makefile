.PHONY: test lint bench fuzz cover

test:
	@go test -race -count=1 ./...

lint:
	@golangci-lint run ./...

bench:
	@go test -bench=. -benchmem ./...

fuzz:
	@go test -fuzz=. -fuzztime=30s .
	@go test -fuzz=FuzzRegex -fuzztime=15s ./ext/
	@go test -fuzz=FuzzJSONSchema -fuzztime=15s ./ext/

cover:
	@go test -coverprofile=coverage.out -covermode=atomic ./...
	@go tool cover -func=coverage.out

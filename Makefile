MODULES := $(shell find . -name go.mod -not -path './vendor/*' -exec dirname {} \; | sort)

.PHONY: test lint bench fuzz cover fix-all lint-all test-all

test:
	@go test -race -count=1 ./...

test-all:
	@for d in $(MODULES); do echo ">>> Testing $$d"; (cd $$d && go test -race -count=1 ./...) || exit 1; done

lint:
	@golangci-lint run ./...

lint-all:
	@for d in $(MODULES); do echo ">>> Linting $$d"; (cd $$d && golangci-lint run ./...) || exit 1; done

fix-all:
	@go work sync
	@for d in $(MODULES); do \
		echo ">>> Fixing $$d"; \
		(cd $$d && go fix ./... && go mod tidy && golangci-lint run --fix ./...) || exit 1; \
	done

bench:
	@go test -bench=. -benchmem ./...

fuzz:
	@go test -fuzz=. -fuzztime=30s .
	@go test -fuzz=FuzzRegex -fuzztime=15s ./ext/

cover:
	@go test -coverprofile=coverage.out -covermode=atomic ./...
	@go tool cover -func=coverage.out

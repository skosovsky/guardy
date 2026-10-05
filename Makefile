GO      := go
GOLANGCI_LINT ?= golangci-lint
MODULES := $(shell find . -type d \( -name ".*" -not -name "." -o -name "vendor" \) -prune -o -type f -name "go.mod" -exec dirname {} \;)

.PHONY: lint fix test bench bench-hotpath fuzz cover release-prepare release-verify release-publish release-test

lint:
	@for dir in $(MODULES); do \
		echo "golangci-lint - $$dir"; \
		(cd "$$dir" && $(GOLANGCI_LINT) run --allow-serial-runners ./...) || exit 1; \
	done

fix:
	@if [ -f "go.work" ]; then $(GO) work sync; fi
	@for dir in $(MODULES); do \
		echo "fix & tidy - $$dir"; \
		(cd "$$dir" && $(GO) fix ./... && $(GO) mod tidy) || exit 1; \
		(cd "$$dir" && $(GOLANGCI_LINT) run --fix ./...) || exit 1; \
	done

test: release-test
	@for dir in $(MODULES); do \
		echo "test - $$dir"; \
		(cd "$$dir" && $(GO) test -v -race ./...) || exit 1; \
	done

bench:
	@for dir in $(MODULES); do \
		echo "bench - $$dir"; \
		(cd "$$dir" && $(GO) test -bench=. -run=^$$ ./...) || exit 1; \
	done

fuzz:
	@for dir in $(MODULES); do \
		echo "fuzz - $$dir"; \
		(cd "$$dir" && \
			for pkg in $$($(GO) list -tags=fuzz ./...); do \
				if $(GO) test -tags=fuzz -list . "$$pkg" 2>/dev/null | grep -q '^Fuzz'; then \
					$(GO) test -tags=fuzz -fuzz=. -fuzztime=30s "$$pkg" || exit 1; \
				fi; \
			done \
		) || exit 1; \
	done

cover:
	@for dir in $(MODULES); do \
		echo "cover - $$dir"; \
		(cd "$$dir" && $(GO) test -coverprofile=coverage.out ./... && $(GO) tool cover -func=coverage.out) || exit 1; \
	done

# Explicit phases: no target performs an automatic push.
release-prepare:
	@test -n "$(VERSION)" && test -n "$(CANDIDATE)"
	@python3 scripts/release.py prepare --version "$(VERSION)" --output "$(CANDIDATE)"

release-verify:
	@test -n "$(CANDIDATE)"
	@python3 scripts/release.py verify "$(CANDIDATE)" --linter "$(GOLANGCI_LINT)"

release-publish:
	@test -n "$(CANDIDATE)" && test -n "$(REMOTE)"
	@python3 scripts/release.py publish "$(CANDIDATE)" --remote "$(REMOTE)"

release-test:
	@python3 -m unittest discover -s scripts -p '*_test.py'

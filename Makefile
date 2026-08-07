.PHONY: build test fmt lint lint-go lint-c hooks

build:
	go build ./...

test:
	go test ./...

fmt:
	gofumpt -w .

lint: lint-go lint-c

lint-go:
	golangci-lint run

# Runs clang-tidy over internal/player's C sources. Uses a container by
# default; set LINT_C_NATIVE=1 to use a locally installed clang-tidy.
lint-c:
	./lint-c.sh

# Activate the tracked git hooks for this clone.
hooks:
	git config core.hooksPath .githooks
	@echo "git hooks activated (.githooks). Bypass a hook with: git commit --no-verify"

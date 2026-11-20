# Generated from github.com/Bam679/agent-cli-plugins/scaffold — keep in sync.
BIN      := paperless
COVER_MIN := 80
GO       ?= go
GOBIN    := $(shell $(GO) env GOPATH)/bin
GOVULNCHECK ?= $(shell command -v govulncheck 2>/dev/null || echo $(GOBIN)/govulncheck)

.PHONY: drift all build fmt fmt-check vet lint test cover fuzz-smoke vuln sec secrets agents-md plugin check agent-check integration snapshot clean tools

all: check

build:
	CGO_ENABLED=0 $(GO) build -trimpath -o bin/$(BIN) ./cmd/$(BIN)

fmt:
	gofmt -w .

fmt-check:
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

vet:
	$(GO) vet ./...
	$(GO) vet -tags integration ./...

lint:
	golangci-lint run ./...

test:
	$(GO) test -race -count=1 -coverprofile=coverage.out -covermode=atomic ./...

cover: test
	@scripts/coverage.sh coverage.out $(COVER_MIN)

fuzz-smoke:
	@scripts/fuzz-smoke.sh 5s

vuln:
	$(GOVULNCHECK) ./...

sec:
	gosec -quiet -exclude-generated ./...

secrets:
	gitleaks git --no-banner --redact .
	gitleaks dir --no-banner --redact .

agents-md:
	@scripts/check-agents-md.sh

plugin:
	@if command -v claude >/dev/null 2>&1; then claude plugin validate . && claude plugin validate plugin --strict; else echo "claude CLI not found: skipping plugin validate (CI runs it)"; fi

# Everything CI checks, locally.
check: fmt-check vet lint cover vuln sec secrets agents-md plugin drift

# Shared scaffold must match agent-cli-plugins (skipped when not checked out next to this repo).
drift:
	@if [ -f ../agent-cli-plugins/scaffold/render.py ]; then python3 ../agent-cli-plugins/scaffold/render.py --check .; else echo "agent-cli-plugins not found next to repo: skipping drift check"; fi

# Fast gate used by the Claude Code Stop hook.
agent-check: fmt-check vet
	$(GO) test -count=1 ./...

# Real macOS / Docker sandbox tests. Creates and removes its own sandbox data.
integration:
	$(GO) test -tags integration -count=1 -v ./...

snapshot:
	goreleaser release --snapshot --clean --skip=sign,sbom

clean:
	rm -rf bin dist coverage.out

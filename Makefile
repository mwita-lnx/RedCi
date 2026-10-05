.PHONY: generate build test lint dev vet tidy clean tools

GOBIN := $(shell go env GOPATH)/bin
TAILWIND := $(GOBIN)/tailwindcss

generate: ## regenerate sqlc, templ and tailwind output
	$(GOBIN)/sqlc generate
	$(GOBIN)/templ generate
	@if [ -x "$(TAILWIND)" ]; then \
		$(TAILWIND) -i ops/web/static/input.css -o ops/web/static/tailwind.css --minify ; \
	else \
		echo "tailwindcss not installed; skipping CSS build (run 'make tools')" ; \
	fi

build: generate ## build both binaries for the host
	CGO_ENABLED=0 go build -o bin/panel ./ops/cmd/panel
	CGO_ENABLED=0 go build -o bin/agent ./agent/cmd/agent
	CGO_ENABLED=0 go build -o bin/ops-certbot ./ops/cmd/ops-certbot

vet:
	go vet ./...

test:
	go test -race ./...

lint:
	$(GOBIN)/staticcheck ./... || true
	$(GOBIN)/golangci-lint run || true

dev: ## run the panel against a local dev.db
	go run ./ops/cmd/panel serve

tidy:
	go mod tidy

clean:
	rm -rf bin dist web/static/tailwind.css

tools: ## install code-generation and lint tooling
	go install github.com/a-h/templ/cmd/templ@latest
	go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
	go install github.com/pressly/goose/v3/cmd/goose@latest
	@echo "Install the Tailwind standalone CLI to $(GOBIN)/tailwindcss separately (platform binary)."

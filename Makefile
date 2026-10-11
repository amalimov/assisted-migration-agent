.PHONY: help build run image run.container stop.container generate check-generate tidy tidy-check lint format check-format validate-all test test.fuzz e2e setup-opa-policies clean

PODMAN ?= podman
BINARY_PATH ?= bin/agent
IMAGE_NAME ?= rvtools-analyser
IMAGE_TAG ?= latest
CONTAINER_NAME ?= rvtools-analyser
AGENT_VOLUME_NAME ?= agent-data
DATA_FOLDER ?= $(CURDIR)/data
OPA_POLICIES_FOLDER ?= $(CURDIR)/policies
PLANNER_AGENT_GIT_COMMIT ?= $(shell git rev-list -1 HEAD --abbrev-commit)
PLANNER_AGENT_VERSION ?= DEV
UI_GIT_COMMIT ?= unknown

help:
	@echo 'build, run, image, run.container, stop.container, test, test.fuzz, e2e, generate, lint, format, tidy, setup-opa-policies'

build:
	@mkdir -p $(dir $(BINARY_PATH))
	go build -ldflags="-X main.gitCommit=$(PLANNER_AGENT_GIT_COMMIT) -X main.version=$(PLANNER_AGENT_VERSION) -X main.uiGitCommit=$(UI_GIT_COMMIT)" -o $(BINARY_PATH) .

run: build
	@mkdir -p $(DATA_FOLDER)
	$(BINARY_PATH) run --data-folder $(DATA_FOLDER) --opa-policies-folder $(OPA_POLICIES_FOLDER)

image:
	$(PODMAN) build --build-arg PLANNER_AGENT_GIT_COMMIT=$(PLANNER_AGENT_GIT_COMMIT) --build-arg PLANNER_AGENT_VERSION=$(PLANNER_AGENT_VERSION) --build-arg UI_GIT_COMMIT=$(UI_GIT_COMMIT) -t $(IMAGE_NAME):$(IMAGE_TAG) -f Containerfile .

run.container:
	$(PODMAN) volume create $(AGENT_VOLUME_NAME)
	$(PODMAN) run -d --name $(CONTAINER_NAME) -p 127.0.0.1:8000:8000 -v $(AGENT_VOLUME_NAME):/var/lib/agent $(IMAGE_NAME):$(IMAGE_TAG)

stop.container:
	$(PODMAN) rm --force $(CONTAINER_NAME)

test:
	go test ./...

e2e:
	go test -count=1 -v ./test/e2e-v2

FUZZ_TIME ?= 30s
test.fuzz:
	go test -race ./internal/filter -fuzz='^FuzzParseSecurityProperties$$' -fuzztime=$(FUZZ_TIME)
	go test -race ./internal/filter -fuzz='^FuzzIdentifierWhitelist$$' -fuzztime=$(FUZZ_TIME)

generate:
	go generate ./api/v2 ./internal/config

check-generate: generate
	@git diff --exit-code -- api/v2/spec.gen.go api/v2/types.gen.go internal/config/zz_generated.configuration.go

tidy:
	go mod tidy

tidy-check: tidy
	@git diff --exit-code -- go.mod go.sum

format:
	gofmt -w $$(rg --files -g '*.go' -g '!local-data/**')

check-format:
	@test -z "$$(gofmt -l $$(rg --files -g '*.go' -g '!local-data/**'))"

GOLANGCI_LINT ?= golangci-lint
lint:
	$(GOLANGCI_LINT) run --timeout=5m

validate-all: lint check-format tidy-check check-generate

setup-opa-policies:
	@mkdir -p $(OPA_POLICIES_FOLDER)
	@tmp=$$(mktemp -d); trap 'rm -rf "$$tmp"' EXIT; \
	  curl -fsSL https://github.com/kubev2v/forklift/archive/main.tar.gz | tar -xz -C "$$tmp" --strip-components=1; \
	  find "$$tmp/validation/policies/io/konveyor/forklift/vmware" -name '*.rego' ! -name '*_test.rego' -exec cp {} $(OPA_POLICIES_FOLDER)/ \;

clean:
	rm -f $(BINARY_PATH)

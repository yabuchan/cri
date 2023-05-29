LINTER_BIN ?= golangci-lint

GO111MODULE := on
export GO111MODULE

BUILD_VERSION    ?= $(shell git describe --tags --always --dirty="-dev")
BUILD_DATE       ?= $(shell date -u '+%Y-%m-%d-%H:%M UTC')
VERSION_FLAGS    := -ldflags='-X "main.buildVersion=$(BUILD_VERSION)" -X "main.buildTime=$(BUILD_DATE)"'

.PHONY: build
build:
	CGO_ENABLED=0 go build -ldflags '-extldflags "-static"' -o bin/$(*) $(VERSION_FLAGS) ./cmd/virtual-kubelet/$(*)

.PHONY: clean
clean: files := bin/virtual-kubelet
clean:
	@rm $(files) &>/dev/null || exit 0

.PHONY: test
test:
	@echo running tests
	go test -v ./...

.PHONY: vet
vet:
	@go vet ./... #$(packages)

.PHONY: lint
lint:
	@$(LINTER_BIN) run --new-from-rev "HEAD~$(git rev-list master.. --count)" ./...

.PHONY: check-mod
check-mod: # verifies that module changes for go.mod and go.sum are checked in
	@hack/ci/check_mods.sh

.PHONY: mod
mod:
	@go mod tidy

.PHONY: docker
docker:
	docker build . -f build/Dockerfile -t 996335758134.dkr.ecr.ap-northeast-1.amazonaws.com/yabusaki/k8s-k3s/virtual-node-v2

bin/virtual-kubelet: BUILD_VERSION          ?= $(shell git describe --tags --always --dirty="-dev")
bin/virtual-kubelet: BUILD_DATE             ?= $(shell date -u '+%Y-%m-%d-%H:%M UTC')
bin/virtual-kubelet: VERSION_FLAGS    := -ldflags='-X "main.buildVersion=$(BUILD_VERSION)" -X "main.buildTime=$(BUILD_DATE)"'

bin/%:
	CGO_ENABLED=0 go build -ldflags '-extldflags "-static"' -o bin/$(*) $(VERSION_FLAGS) ./cmd/$(*)

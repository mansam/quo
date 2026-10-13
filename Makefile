# Build and test quo. The default target builds the binary into bin/.
#
# The test targets match what CI runs, so a green `make check` here is the
# same work the test workflow does on a pull request. The live suite needs a
# KubeVirt cluster and is kept out of `check` for that reason; see
# docs/testing-against-a-cluster.md.

GO ?= go
BIN_DIR ?= bin
BIN ?= $(BIN_DIR)/quo

.PHONY: all
all: build

# The quo binary.
.PHONY: build
build:
	$(GO) build -o $(BIN) ./cmd/quo

# Install quo into the Go bin directory, so it is on the path.
.PHONY: install
install:
	$(GO) install ./cmd/quo

# Everything CI runs that needs no cluster.
.PHONY: check
check: vet test vet-live build-live

.PHONY: test
test:
	$(GO) test ./...

.PHONY: vet
vet:
	$(GO) vet ./...

.PHONY: fmt
fmt:
	$(GO) fmt ./...

# The live suite is excluded from the default test run, so it is vetted and
# compiled here to catch a change that stops it building.
.PHONY: vet-live
vet-live:
	$(GO) vet -tags live ./...

.PHONY: build-live
build-live:
	$(GO) test -tags live -run '^$$' ./test/live/...

# The live suite itself. This needs a KubeVirt cluster with the ObjectGraph
# feature gate enabled, named by QUO_LIVE_KUBECONFIG. Tests that need a
# cluster skip and say so when there is none.
.PHONY: test-live
test-live:
	$(GO) test -tags live -v -timeout 40m ./test/live/...

.PHONY: clean
clean:
	rm -rf $(BIN_DIR)
	$(GO) clean -testcache

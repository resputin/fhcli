.DEFAULT_TARGET := build

REVISION := $(shell cat git-revision 2>/dev/null || git rev-parse HEAD)
RELEASE_VERSION := $(shell cat version)

build:
	go build -ldflags "-X main.commit=$(REVISION) -X main.version=$(RELEASE_VERSION)" -o fhcli cmd/main.go

install: build
	install -m 0755 fhcli $(shell go env GOPATH)/bin/fhcli

spec:
	curl -fsSL https://api.firehydrant.io/v1/swagger_doc -o pkg/spec/spec.json

# Run the test suite against the live API spec instead of the embedded
# snapshot, to catch spec changes that break `fhcli api` or `fhcli search`
# before running `make spec`.
test-live:
	@tmp=$$(mktemp -t fhcli-live-spec) && \
	curl -fsSL https://api.firehydrant.io/v1/swagger_doc -o $$tmp && \
	FHCLI_TEST_SPEC=$$tmp go test -count=1 ./... ; status=$$?; rm -f $$tmp; exit $$status

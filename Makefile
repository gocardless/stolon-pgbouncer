PROG=bin/stolon-pgbouncer
PROJECT=github.com/gocardless/stolon-pgbouncer
VERSION=$(shell git rev-parse --short HEAD)-dev
BUILD_COMMAND=go build -ldflags "-X main.Version=$(VERSION)"

.PHONY: all darwin linux test clean

all: darwin linux
darwin: $(PROG)
linux: $(PROG:=.linux_amd64)

bin/%.linux_amd64:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(BUILD_COMMAND) -a -o $@ cmd/$*/main.go

bin/%:
	$(BUILD_COMMAND) -o $@ cmd/$*/main.go

generate:
	go generate ./...

# go get -u github.com/onsi/ginkgo/ginkgo
test:
	ginkgo -v -r

# Produces test binaries for CI
build-test:
	ginkgo build -r -race .

clean:
	rm -rvf $(PROG) $(PROG:%=%.linux_amd64)

BASE_TAG=2019040101
STOLON_NODE_TAG=2019040101

docker-base: docker/base/Dockerfile
	docker build -t gocardless/stolon-pgbouncer-base:$(BASE_TAG) docker/base

docker-stolon-node: docker/stolon-node/Dockerfile
	docker build -t gocardless/stolon-node:$(STOLON_NODE_TAG) docker/stolon-node

publish-base: docker-base
	docker push gocardless/stolon-pgbouncer-base:$(BASE_TAG)

publish-stolon-node: docker-stolon-node
	docker push gocardless/stolon-node:$(STOLON_NODE_TAG)

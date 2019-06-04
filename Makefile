PROG=bin/stolon-pgbouncer bin/stolon-pgbouncer-acceptance
PROJECT=github.com/gocardless/stolon-pgbouncer
VERSION=$(shell git rev-parse --short HEAD)-dev
BUILD_COMMAND=GO111MODULE=on go build -ldflags "-X main.Version=$(VERSION)"

BASE_TAG=2019040201
CIRCLECI_TAG=2019040303
STOLON_DEVELOPMENT_TAG=2019060300

.PHONY: all darwin linux test clean test-acceptance docker-compose

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

test-acceptance: docker-compose
	go run cmd/stolon-pgbouncer-acceptance/main.go

clean:
	rm -rvf $(PROG) $(PROG:%=%.linux_amd64)

docker-compose: clean bin/stolon-pgbouncer.linux_amd64
	docker-compose up --no-start
	docker-compose start

docker-base: docker/base/Dockerfile
	docker build -t gocardless/stolon-pgbouncer-base:$(BASE_TAG) docker/base

docker-circleci: docker/circleci/Dockerfile
	docker build -t gocardless/stolon-pgbouncer-circleci:$(CIRCLECI_TAG) docker/circleci

docker-stolon-development: docker/stolon-development/Dockerfile
	docker build -t gocardless/stolon-development:$(STOLON_DEVELOPMENT_TAG) docker/stolon-development

publish-base: docker-base
	docker push gocardless/stolon-pgbouncer-base:$(BASE_TAG)

publish-circleci: docker-circleci
	docker push gocardless/stolon-pgbouncer-circleci:$(CIRCLECI_TAG)

publish-stolon-development: docker-stolon-development
	docker push gocardless/stolon-development:$(STOLON_DEVELOPMENT_TAG)

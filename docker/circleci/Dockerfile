# In addition to our base install of pgbouncer and postgresql-client, add CI
# dependencies that we require during our builds.
FROM gocardless/stolon-pgbouncer-base:2022042601

# General test utilities
RUN set -x \
      && apt-get update -y \
      && apt-get install -y curl git make build-essential

# Go is required to compile our binaries and run our tests. This includes ginkgo
# as a test runner.
ENV GOPATH=/go GOROOT=/usr/local/go PATH=$PATH:/usr/local/go/bin:/go/bin:/usr/sbin
RUN set -x \
      && mkdir -p /usr/local/go /go \
      && curl -L https://dl.google.com/go/go1.17.linux-amd64.tar.gz -o /tmp/go.tar.gz \
      && tar xfvz /tmp/go.tar.gz -C /usr/local/go --strip-components=1 \
      && go version \
      && go get -v -u github.com/onsi/ginkgo/v2 \
      && go install github.com/onsi/ginkgo/v2/ginkgo@latest \
      && ginkgo version \
      && rm -rv /tmp/go.tar.gz

# We require etcd for our integration tests
RUN set -x \
      && curl -fsL https://storage.googleapis.com/etcd/v3.3.12/etcd-v3.3.12-linux-amd64.tar.gz -o /tmp/etcd.tar.gz \
      && tar xfvz /tmp/etcd.tar.gz -C /usr/local/bin --wildcards 'etcd-*-linux-amd64/etcd' --wildcards 'etcd-*-linux-amd64/etcdctl' --strip-components=1 \
      && rm -v /tmp/etcd.tar.gz

# goreleaser is used to deploy new releases
RUN set -x \
      && curl -fsL https://github.com/goreleaser/goreleaser/releases/download/v0.101.0/goreleaser_Linux_x86_64.tar.gz -o /tmp/goreleaser.tar.gz \
      && tar xfvz /tmp/goreleaser.tar.gz -C /usr/local/bin --wildcards 'goreleaser' \
      && rm -v /tmp/goreleaser.tar.gz

# docker is required to build the release images
RUN set -x \
      && curl "https://download.docker.com/linux/static/stable/x86_64/docker-17.06.2-ce.tgz" -o /tmp/docker.tar.gz \
      && tar xfvz /tmp/docker.tar.gz -C /usr/local/bin docker/docker --strip-components=1 \
      && rm -v /tmp/docker.tar.gz

# The acceptance test uses these environment variables
ENV ETCDCTL_API=3 \
    CLUSTER_NAME=main \
    STOLONCTL_CLUSTER_NAME=main \
    STORE_BACKEND=etcdv3 \
    STOLONCTL_STORE_BACKEND=etcdv3 \
    STORE_ENDPOINTS=etcd-store:2379 \
    STOLONCTL_STORE_ENDPOINTS=etcd-store:2379

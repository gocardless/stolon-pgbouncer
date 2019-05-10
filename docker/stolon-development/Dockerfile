# Temporarily use the GoCardless stolon fork to install the keeper. This enables
# us to verify the metrics we're adding to the binaries.
FROM golang:1.12 AS stolon-fork
RUN set -x \
      && go get -d -v github.com/sorintlab/stolon/cmd \
      && cd "${GOPATH}/src/github.com/sorintlab/stolon" \
      && git remote add gocardless https://github.com/gocardless/stolon \
      && git fetch gocardless \
      && git checkout 71d109541f800fcacb2d8e3222715d84e569ccb5 \
      && go build -o stolon-keeper cmd/keeper/main.go


# In addition to our base install of pgbouncer and postgresql-client, configure
# all the dependencies we'll need across our docker-compose setup along with
# convenience env vars to make stolon tooling function correctly.
FROM gocardless/stolon-pgbouncer-base:2019040201

RUN set -x \
      && apt-get install -y curl etcd-client supervisor postgresql-11 \
      && curl -fsL https://github.com/sorintlab/stolon/releases/download/v0.13.0/stolon-v0.13.0-linux-amd64.tar.gz -o /tmp/stolon.tar.gz \
      && tar xfvz /tmp/stolon.tar.gz -C /usr/local/bin --wildcards '*/bin/*' --strip-components=2 \
      && rm -v /tmp/stolon.tar.gz

# Load the GoCardless keeper fork
COPY --from=stolon-fork /go/src/github.com/sorintlab/stolon/stolon-keeper /usr/local/bin/stolon-keeper

ENV ETCDCTL_API=3 \
    CLUSTER_NAME=main \
    STOLONCTL_CLUSTER_NAME=main \
    STORE_BACKEND=etcdv3 \
    STOLONCTL_STORE_BACKEND=etcdv3 \
    STORE_ENDPOINTS=etcd-store:2379 \
    STOLONCTL_STORE_ENDPOINTS=etcd-store:2379 \
    STBOUNCER_FAILOVER_TOKEN=failover-token

# Cluster data is placed here, and required to be Postgres writeable
RUN mkdir /data && chown -R postgres:postgres /data

# 5432 => Postgres
# 6432 => PgBouncer
# 7432 => stolon-proxy
# 8080 => stolon-pgbouncer (pauser)
# 9459 => stolon-keeper (metrics)
# 9446 => stolon-pgbouncer (metrics)
EXPOSE 5432 6432 7432 8080 9459 9446
ENTRYPOINT ["supervisord", "-n", "-c", "/stolon-pgbouncer/docker/stolon-development/supervisord.conf"]

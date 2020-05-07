# Temporarily use the GoCardless stolon fork to install the keeper. This enables
# us to verify the metrics we're adding to the binaries.
FROM golang:1.14 AS stolon-fork
RUN set -x \
      && go get -d -v github.com/sorintlab/stolon/cmd \
      && cd "${GOPATH}/src/github.com/sorintlab/stolon" \
      && git remote add gocardless https://github.com/gocardless/stolon \
      && git fetch gocardless \
      && git checkout b2e9e04434e0350356e682dc725bb1551b33784a \
      && ./build

# GoCardless runs this fork for PgBouncer metrics. We'll likely change this in
# future but include it for now so the dashboards in this repo can match what we
# have deployed internally.
FROM golang:1.14 AS pgbouncer-exporter-fork
RUN set -x \
      && go get -d -v github.com/gocardless/pgbouncer_exporter \
      && cd "${GOPATH}/src/github.com/gocardless/pgbouncer_exporter" \
      && git checkout 7bbdf2573e6b6fe4bc6fe3981173ccd338763771 \
      && make

# In addition to our base install of pgbouncer and postgresql-client, configure
# all the dependencies we'll need across our docker-compose setup along with
# convenience env vars to make stolon tooling function correctly.
FROM gocardless/stolon-pgbouncer-base:2020050701
ENV DEBIAN_FRONTEND noninteractive

RUN set -x \
      && apt-get update -y \
      && apt-get install --no-install-recommends -y curl etcd-client supervisor postgresql-11

COPY --from=stolon-fork \
  /go/src/github.com/sorintlab/stolon/bin/stolon-keeper \
  /go/src/github.com/sorintlab/stolon/bin/stolon-proxy \
  /go/src/github.com/sorintlab/stolon/bin/stolon-sentinel \
  /go/src/github.com/sorintlab/stolon/bin/stolonctl \
  /usr/local/bin/

COPY --from=pgbouncer-exporter-fork /go/src/github.com/gocardless/pgbouncer_exporter/pgbouncer_exporter /usr/local/bin/pgbouncer_exporter

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
# 9127 => pgbouncer_exporter (metrics)
# 9459 => stolon-keeper (metrics)
# 9446 => stolon-pgbouncer (metrics)
EXPOSE 5432 6432 7432 8080 9127 9459 9446
ENTRYPOINT ["supervisord", "-n", "-c"]

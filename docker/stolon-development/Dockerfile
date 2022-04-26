# Temporarily use the GoCardless stolon fork to install the keeper. This enables
# us to verify the metrics we're adding to the binaries.
FROM golang:1.17.8 AS stolon-fork
RUN set -x \
      && git clone https://github.com/gocardless/stolon.git \
      && cd stolon \
      && git checkout dba0fe05a317ca9d485b8d15f27ecae1bb39b180 \
      && make all

# GoCardless runs this fork for PgBouncer metrics. We'll likely change this in
# future but include it for now so the dashboards in this repo can match what we
# have deployed internally.
# TODO: update our fork
FROM golang:1.17.8 AS pgbouncer-exporter
RUN set -x \
      && git clone https://github.com/prometheus-community/pgbouncer_exporter.git \
      && cd pgbouncer_exporter \
      && git checkout 6f7e6de674d3b7d412a5960b7d2e849e40c1d76b \
      && make build

# In addition to our base install of pgbouncer and postgresql-client, configure
# all the dependencies we'll need across our docker-compose setup along with
# convenience env vars to make stolon tooling function correctly.
FROM gocardless/stolon-pgbouncer-base:2022042601
ENV DEBIAN_FRONTEND noninteractive

RUN set -x \
      && apt-get update -y \
      && apt-get install --no-install-recommends -y curl etcd-client supervisor postgresql-14

COPY --from=stolon-fork \
  /go/stolon/bin/stolon-keeper  \
  /go/stolon/bin/stolon-proxy \
  /go/stolon/bin/stolon-sentinel \
  /go/stolon/bin/stolonctl \
  /usr/local/bin/

COPY --from=pgbouncer-exporter /go/pgbouncer_exporter/pgbouncer_exporter /usr/local/bin/pgbouncer_exporter

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

# Cluster WAL data may be placed here, and required to be Postgres writeable
RUN mkdir /wal && chown -R postgres:postgres /wal

# 5432 => Postgres
# 6432 => PgBouncer
# 7432 => stolon-proxy
# 8080 => stolon-pgbouncer (pauser)
# 9127 => pgbouncer_exporter (metrics)
# 9459 => stolon-keeper (metrics)
# 9446 => stolon-pgbouncer (metrics)
EXPOSE 5432 6432 7432 8080 9127 9459 9446
ENTRYPOINT ["supervisord", "-n", "-c"]

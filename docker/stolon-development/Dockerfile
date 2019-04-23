# In addition to our base install of pgbouncer and postgresql-client, configure
# all the dependencies we'll need across our docker-compose setup along with
# convenience env vars to make stolon tooling function correctly.
FROM gocardless/stolon-pgbouncer-base:2019040201

RUN set -x \
      && apt-get install -y curl etcd-client supervisor postgresql-11 \
      && curl -fsL https://github.com/sorintlab/stolon/releases/download/v0.13.0/stolon-v0.13.0-linux-amd64.tar.gz -o /tmp/stolon.tar.gz \
      && tar xfvz /tmp/stolon.tar.gz -C /usr/local/bin --wildcards '*/bin/*' --strip-components=2 \
      && rm -v /tmp/stolon.tar.gz

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

EXPOSE 5432 6432 7432 8080 9446
ENTRYPOINT ["supervisord", "-n", "-c", "/stolon-pgbouncer/docker/stolon-development/supervisord.conf"]

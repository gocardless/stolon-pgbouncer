# Runs on-top of the stolon-pgbouncer image to install an appropriate test
# Postgres setup
FROM gocardless/stolon-pgbouncer:v1

# Change back to root to install our dependencies
USER root

RUN set -x \
      && apt-get install -y sudo curl etcd-client supervisor \
      && sh -c 'echo "deb http://apt.postgresql.org/pub/repos/apt/ $(lsb_release -cs)-pgdg main\ndeb http://apt.postgresql.org/pub/repos/apt/ $(lsb_release -cs)-pgdg 11" > /etc/apt/sources.list.d/pgdg.list' \
      && curl --silent https://www.postgresql.org/media/keys/ACCC4CF8.asc | sudo apt-key add - \
      && apt-get update -y \
      && apt-get install -y postgresql-11 \
      && curl -fsL https://github.com/sorintlab/stolon/releases/download/v0.13.0/stolon-v0.13.0-linux-amd64.tar.gz -o /tmp/stolon.tar.gz \
      && tar xfvz /tmp/stolon.tar.gz -C /usr/local/bin --wildcards '*/bin/*' --strip-components=2 \
      && rm -v /tmp/stolon.tar.gz

ENV ETCDCTL_API=3 \
    CLUSTER_NAME=main \
    STOLONCTL_CLUSTER_NAME=main \
    STORE_BACKEND=etcdv3 \
    STOLONCTL_STORE_BACKEND=etcdv3 \
    STORE_ENDPOINTS=etcd-store:2379 \
    STOLONCTL_STORE_ENDPOINTS=etcd-store:2379

# Cluster data is placed here, and required to be Postgres writeable
RUN mkdir /data && chown -R postgres:postgres /data

# We rely on our mounted binary and remove this to prevent people from getting
# confused.
RUN rm -v /usr/local/bin/stolon-pgbouncer

EXPOSE 5432 6432 7432 8080
ENTRYPOINT ["supervisord", "-n", "-c", "/stolon-pgbouncer/docker/stolon-node/supervisord.conf"]

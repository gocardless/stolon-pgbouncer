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

COPY --chown=postgres pgbouncer/userlist.txt pgbouncer/pgbouncer.ini.template pgbouncer/pgbouncer.ini /etc/pgbouncer/
COPY stolon/specification.json stolon/init.bash /etc/stolon/

# Cluster data is placed here, and required to be Postgres writeable
RUN mkdir /data && chown -R postgres:postgres /data

EXPOSE 5432 6432 7432 8080
ADD supervisord.conf /
ENTRYPOINT ["supervisord", "-n", "-c", "/supervisord.conf"]

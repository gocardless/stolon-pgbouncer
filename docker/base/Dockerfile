FROM ubuntu:bionic-20200403
RUN set -x \
      && apt-get update -y \
      && apt-get install -y curl gpg \
      && sh -c 'echo "deb http://apt.postgresql.org/pub/repos/apt/ bionic-pgdg main\ndeb http://apt.postgresql.org/pub/repos/apt/ bionic-pgdg 11" > /etc/apt/sources.list.d/pgdg.list' \
      && curl --silent https://www.postgresql.org/media/keys/ACCC4CF8.asc | apt-key add - \
      && apt-get update -y \
      && apt-get install -y software-properties-common pgbouncer postgresql-client \
      && mkdir -pv /var/run/postgresql /var/log/postgresql

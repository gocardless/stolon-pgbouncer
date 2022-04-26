FROM ubuntu:focal-20220302
ENV DEBIAN_FRONTEND noninteractive
RUN set -x \
      && apt-get update -y \
      && apt-get install -y curl gpg \
      && sh -c 'echo "deb http://apt.postgresql.org/pub/repos/apt/ focal-pgdg main\ndeb http://apt.postgresql.org/pub/repos/apt/ focal-pgdg 14" > /etc/apt/sources.list.d/pgdg.list' \
      && curl --silent https://www.postgresql.org/media/keys/ACCC4CF8.asc | apt-key add - \
      && apt-get update -y \
      && apt-get install -y software-properties-common pgbouncer postgresql-client \
      && mkdir -pv /var/run/postgresql /var/log/postgresql

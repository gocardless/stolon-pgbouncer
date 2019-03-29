FROM golang:1.12 AS build
COPY . /go/src/github.com/gocardless/stolon-pgbouncer
WORKDIR /go/src/github.com/gocardless/stolon-pgbouncer
RUN go build -o stolon-pgbouncer cmd/stolon-pgbouncer/main.go

FROM ubuntu:18.04
RUN set -x \
      && apt-get update -y \
      && apt-get install -y software-properties-common pgbouncer postgresql-client \
      && mkdir -pv /var/run/postgresql /var/log/postgresql

COPY --from=build /go/src/github.com/gocardless/stolon-pgbouncer/stolon-pgbouncer /usr/local/bin/stolon-pgbouncer
USER app
ENTRYPOINT ["/usr/local/bin/stolon-pgbouncer"]

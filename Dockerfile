################################################################################
# build
################################################################################

FROM golang:1.17.8 AS build
COPY . /go/src/github.com/gocardless/stolon-pgbouncer
WORKDIR /go/src/github.com/gocardless/stolon-pgbouncer

# If we're running goreleaser, then our binary will already be copied into our
# work directory. Otherwise we should generate it with make.
RUN set -x \
      && \
      if [ ! -f stolon-pgbouncer ]; then \
        make bin/stolon-pgbouncer; \
        mv -v bin/stolon-pgbouncer stolon-pgbouncer; \
      fi

################################################################################
# release
################################################################################

FROM gocardless/stolon-pgbouncer-base:2022042601 AS release
COPY --from=build /go/src/github.com/gocardless/stolon-pgbouncer/stolon-pgbouncer /usr/local/bin/stolon-pgbouncer
USER postgres
ENTRYPOINT ["/usr/local/bin/stolon-pgbouncer"]

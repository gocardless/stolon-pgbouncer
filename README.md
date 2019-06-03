# stolon-pgbouncer [![CircleCI](https://circleci.com/gh/gocardless/stolon-pgbouncer/tree/master.svg?style=svg)](https://circleci.com/gh/gocardless/stolon-pgbouncer/tree/master)

stolon-pgbouncer extends a [stolon](https://github.com/sorintlab/stolon)
PostgreSQL setup with PgBouncer connection pooling and zero-downtime planned
failover of the PostgreSQL primary.

See [Playground](#playground) for how to start a Dockerised three node stolon
PostgreSQL cluster utilising stolon-pgbouncer.

- [Overview](#overview)
  - [Stolon Recap](#stolon-recap)
  - [Playground](#playground)
  - [Node Roles](#node-roles)
    - [Postgres](#postgres)
    - [Proxy](#proxy)
  - [Zero-Downtime Failover](#zero-downtime-failover)
- [Development](#development)
  - [Testing](#testing)
  - [Images](#images)
  - [Releasing](#releasing)

## Overview

[stolon](https://github.com/sorintlab/stolon) is a tool for running highly
available Postgres clusters. stolon aims to recover from node failures and
ensure data durability across your cluster.

stolon-pgbouncer extends stolon with first class support for PgBouncer, a
Postgres connection pooler. By introducing PgBouncer, it's possible to offer
zero-downtime planned failovers of Postgres primaries, allowing users to perform
maintenance operations without taking downtime.

Live information about cluster health is maintained by stolon in a consistent
data store such as etcd. stolon-pgbouncer runs two services that use this data
to work with the cluster:

- `supervise` manages PgBouncer processes to proxy connections to the currently
  elected Postgres primary
- `pauser` exposes an API that can perform zero-downtime failover by pausing
  PgBouncer traffic

Both these services are commands on the `stolon-pgbouncer` binary, with a third
command called `failover` which speaks with the pauser API.

### Stolon Recap

This README assumes familiarity with stolon and associated tooling that can be
acquired by reading the [stolon docs](https://github.com/sorintlab/stolon/blob/master/doc/architecture.md).
While we advise you read these first, we'll summarise each stolon component for
convenience here:

- `keeper` supervises, configures, and converges PostgreSQL on each PostgreSQL
  node according to the clusterview
- `sentinel` discovers and monitors the keepers, and calculates the optimal
  clusterview
- `proxy` ensures connections are pointing to the master PostgreSQL node and fences
  (forcibly closes connections) to unelected masters

We use these terms throughout this README, and encourage referring to the stolon
docs whenever anything is unclear.

### Playground

We have created a Dockerised sandbox environment that boots a three node
Postgres cluster with the stolon-pgbouncer services installed, using etcd as our
consistent stolon store. We recommend playing around in this environment to
develop an understanding of how this setup works and to simulate failure
situations (network partitions, node crashes, etc).

**It also helps to have this playground running while reading through the
README, in order to try out the commands you see along the way.**

First install [Docker](https://docker.io/) and Golang >=1.12, then run:

```
# Clone into your GOPATH
$ git clone https://github.com/gocardless/stolon-pgbouncer
$ cd stolon-pgbouncer
$ make docker-compose
...

# List all docker-compose services
$ docker-compose ps
    Name                Command                         Ports
------------------------------------------------------------------------------------
etcd-store_1   etcd --data-dir=/data --li ...   0.0.0.0:2379->2379, 2380
keeper0_1      supervisord -n -c /stolon- ...   5432, 0.0.0.0:6433->6432, 7432, 8080
keeper1_1      supervisord -n -c /stolon- ...   5432, 0.0.0.0:6434->6432, 7432, 8080
keeper2_1      supervisord -n -c /stolon- ...   5432, 0.0.0.0:6435->6432, 7432, 8080
pgbouncer_1    /stolon-pgbouncer/bin/stol ...   5432, 0.0.0.0:6432->6432, 7432, 8080
sentinel_1     /usr/local/bin/stolon-sent ...   5432, 6432, 7432, 8080

# Query clusterview for status
$ docker exec stolon-pgbouncer_pgbouncer_1 stolonctl status
=== Keepers ===

UID     HEALTHY PG LISTENADDRESS        PG HEALTHY
keeper0 true    172.24.0.4:5432         true
keeper1 true    172.24.0.5:5432         true
keeper2 true    172.24.0.6:5432         true

...
```

### Node Roles

In a stolon-pgbouncer cluster, you will typically run two types of nodes: the
Postgres nodes where we run the keeper/Postgres/PgBouncer, and the proxy nodes
that run a supervised PgBouncer that provides connectivity to our cluster (this
is what applications will connect via).

![Playground architecture](resources/playground-architecture.svg)

In our playground setup we run a single proxy node (called pgbouncer in our
docker-compose) and three Postgres nodes (`keeper0`, `keeper1`, `keeper2`)
which- in addition to the keeper and Postgres- run the stolon-pgbouncer pauser.

#### Postgres

The Postgres node role is provisioned to run the stolon keeper (and therefore
Postgres) and proxy on the same machines, exposing our Postgres service via a
PgBouncer. Incoming database connections should only ever arrive via the
PgBouncer service, which in turn will point at the host-local proxy.

We leverage stolon's fencing by directing connections through the proxy, which
will terminate clients in the case of failover. PgBouncer is placed in front of
our proxy to provide pausing for planned failover, as existing client
connections need to be paused before we move the Postgres primary.

The intention is for all cluster connections to be routed to just one PgBouncer
at any one time, and for that PgBouncer to be co-located with our primary to
avoid unnecessary network hops. While you could connect via any of the keeper
node PgBouncers, our stolon-pgbouncer `supervise` processes will ensure we
converge on the primary.

#### Proxy

Proxy nodes can be run separately from our Postgres cluster, ideally close to
wherever the application that uses Postgres is located. These nodes run
stolon-pgbouncers `supervise` service which manages a PgBouncer to point at the
current primary. Our aim is to have applications connect to our PgBouncer
service and be routed to the PgBouncer that exists on the Postgres nodes.

To do this, we provision proxy nodes with PgBouncer and a templatable
configuration file that looks like this:

```ini
# /etc/pgbouncer/pgbouncer.ini.template
[databases]
postgres = host={{.Host}} port=6432
```

Whenever the clusterview (managed by our stolon sentinels) changes, the
stolon-pgbouncer supervise process will respond by templating our
`pgbouncer.ini` config with the IP address of our elected primary. Application
connects will be re-routed to the current primary, where we expect them to
connect to PgBouncer (port 6432).

### Zero-Downtime Failover

stolon-pgbouncer provides ability to failover cluster nodes without
impacting traffic. We do this by exposing an API on the Postgres nodes that can
pause database connections before instructing stolon to elect a new node as the
cluster primary.

This API is served by the `supervise` service, which should run on all the
Postgres nodes participating in the cluster. It's important to note that this
flow is only supported when all database clients are using PgBouncer transaction
pools in order to support pausing connections. Any clients that use session
pools will need to be turned off for the duration of the failover.

The failover process is as follows:

1. Confirm cluster is healthy and can survive a node failure
1. Acquire lock in etcd (ensuring only one failover takes place at a time)
1. Pause all PgBouncer pools on Postgres nodes
1. Mark primary keeper as unhealthy
1. Once stolon has elected a new primary, resume PgBouncer pools
1. Release etcd lock

This flow is encoded in the [`Run`](pkg/failover/failover.go) method,
and looks like this:

```go
Pipeline(
  Step(f.CheckClusterHealthy),
  Step(f.HealthCheckClients),
  Step(f.AcquireLock).Defer(f.ReleaseLock),
  Step(f.Pause).Defer(f.Resume),
  Step(f.Failkeeper),
)
```

Once the new primary is ready, our Proxy nodes running stolon-pgbouncer's
`supervise` will template a new PgBouncer configuration that points at the new
master. Connections will resume their operation unaware that they now speak to a
different Postgres server than before.

Running the failover within the playground environment looks like this:

```
ts=31 event=metrics.listen address=127.0.0.1 port=9446
ts=31 event=client_dial client="keeper2 (172.27.0.4)"
ts=31 event=client_dial client="keeper1 (172.27.0.6)"
ts=31 event=client_dial client="keeper0 (172.27.0.5)"
ts=31 event=setting_pauser_token
ts=31 event=check_cluster_healthy msg="checking health of cluster"
ts=31 event=clients_health_check msg="health checking all clients"
ts=31 event=etcd_lock_acquire msg="acquiring failover lock in etcd"
ts=31 event=pgbouncer_pause msg="requesting all pgbouncers pause"
ts=31 event=pgbouncer_pause endpoint=keeper0 elapsed=0.0023349
ts=31 event=pgbouncer_pause endpoint=keeper2 elapsed=0.0095867
ts=31 event=pgbouncer_pause endpoint=keeper1 elapsed=0.0116491
ts=31 key=stolon/cluster/main/clusterdata msg="waiting for stolon to report master change"
ts=31 keys=stolon/cluster/main/clusterdata event=watch.start
ts=31 keys=stolon/cluster/main/clusterdata event=poll.start
ts=31 key=stolon/cluster/main/clusterdata event=pending_failover master="keeper2 (172.27.0.4)" msg="master has not changed nodes"
ts=36 keys=stolon/cluster/main/clusterdata event=poll.start
ts=36 key=stolon/cluster/main/clusterdata event=insufficient_standbys healthy=0 minimum=1 msg="do not have enough healthy standbys to satisfy the minSynchronousStandbys"
ts=41 keys=stolon/cluster/main/clusterdata event=poll.start
ts=41 key=stolon/cluster/main/clusterdata master="keeper0 (172.27.0.5)" msg="master is available for writes"
ts=41 msg="cluster successfully recovered" master="keeper0 (172.27.0.5)"
ts=41 event=pgbouncer_resume msg="requesting all pgbouncers resume"
ts=41 event=pgbouncer_resume endpoint=keeper1 elapsed=0.0029219
ts=41 event=pgbouncer_resume endpoint=keeper0 elapsed=0.00493
ts=41 event=pgbouncer_resume endpoint=keeper2 elapsed=0.0124522
ts=41 event=etcd_lock_release msg="releasing failover lock in etcd"
ts=41 event=shutdown
```

This flow is subject to several timeouts that need configuring to suit your
production environment. Pause expiry is notable as it needs pairing with load
balancer timeouts to ensure you don't drop requests. See the stolon-pgbouncer
`--help` for more details.

## Development

### Testing

stolon-pgbouncer uses [ginkgo](https://github.com/onsi/ginkgo) and
[gomega](https://onsi.github.io/gomega/) for testing. Tests are grouped into
three categories:

- unit, co-located with the Go package they target, relying on no external
  dependencies (you could run these tests in a scratch container with no
  external tools and they should succeed)
- integration, placed within an `integration` folder inside the Go package
  directory they target. Integration tests can assume access to an external
  Postgres database along with PgBouncer and etcd binaries and will directly
  boot and manage these dependencies
- acceptance, written as a standalone binary build from
  `cmd/stolon-pgbouncer-acceptance/main.go`. This environment assumes you have
  booted the docker-compose playground

For those developing stolon-pgbouncer, we advise configuring your dev machine to
be suitable for the integration environment and testing via `ginkgo -r`. All
tests are run in CI as a final check before merge: refer to the
[`circle.yml`](circle.yml) file as a complete reference for a test environment.

### Images

We use several docker images to power our CI and development environments. See
the [README](docker) to understand what each image is for.

Each image can be built and published using a Makefile target, and we generate
tags as `YYYYMMDDXX` where `XX` is an index into the current day. An example of
publishing a new base image is:

```
$ make publish-base
```

### Releasing

We use [goreleaser](https://github.com/goreleaser/goreleaser) to create
releases and publish docker images. Just update the [`VERSION`](VERSION) file
with the new version and push to master.

Our versioning system follows [semver guidelines](https://semver.org/) and care
should be taken to adhere to these rules.

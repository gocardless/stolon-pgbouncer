# stolon-pgbouncer

## Tests

Run unit tests:
    make test

Run acceptance tests:
    make test-acceptance

This will spin up a cluster using docker-compose and run acceptance tests
against it.  Right now there's a race between the cluster becoming stable and
the test starting, so it may be a bit flaky.

## Development

We use docker-compose to provide a development environment for a full stolon
cluster, with stolon-pgbouncer installed.

Running `docker-compose up` will start all the necessary dependencies.

### Using stolonctl

If you set the following environment variables then stolonctl can run from your
local machine:

```
export STOLONCTL_CLUSTER_NAME=main
export STOLONCTL_STORE_BACKEND=etcdv3

$ stolonctl status
=== Active sentinels ===

ID              LEADER
3d996ae6        true

...
```

### Connecting to stolon-pgbouncer

The PgBouncer managed by stolon-pgbouncer is exposed on port 6432 of your host
machine. You can connect to this PgBouncer like so:

```
$ psql -h localhost -p 6432 -U postgres postgres
psql (11.1, server 11.2 (Ubuntu 11.2-1.pgdg18.04+1))
Type "help" for help.

postgres=# select inet_server_addr();
 inet_server_addr
------------------
 172.18.0.6
(1 row)
```

The result of `inet_server_addr()` will be the IP address of the host that this
PgBouncer is proxying to. We expect that to the be the stolon primary. You can
also connect to the PgBouncer admin controls of the stolon-pgbouncer managed
process like so:

```
$ psql -h localhost -p 6432 -U pgbouncer pgbouncer
psql (11.1, server 1.9.0/bouncer)
Type "help" for help.

pgbouncer=# show help;
NOTICE:  Console usage
DETAIL:
        SHOW HELP|CONFIG|DATABASES|POOLS|CLIENTS|SERVERS|VERSION
        SHOW STATS|STATS_TOTALS|STATS_AVERAGES
        RELOAD
        PAUSE [<db>]
        ...
```

### Connecting to keepers

The keeper nodes have the PgBouncer ports exposed via docker-compose onto the
host ports of 6433, 6434 and 6435 (keeper0, keeper1, keeper2). You can access
them via these ports on your local machine:
```
# Access keeper0 PgBouncer from local machine
$ psql -h localhost -p 6433 -U postgres postgres -c "select 'keeper'" -t
 keeper
```

If you want to access services other than PgBouncer on the keeper nodes then
exec into the container:

```
# From within the keeper container
$ docker-compose exec keeper0 psql -p 6432 -U postgres -c "select 'keeper'" -t
 keeper
```

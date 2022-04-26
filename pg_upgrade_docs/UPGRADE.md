
This process assumes keeper2 is the primary.

steps:

```
// ssh into the primary machine
make docker-stolon-development
make docker-compose
docker-compose exec keeper2 /bin/bash

// load some data
su postgres
psql -h /tmp -c 'CREATE DATABASE example;'
pgbench -h /tmp -i -s 200 example

// Initialize the new directories. 
// Note: should be the same config as prev directory
/usr/lib/postgresql/14/bin/initdb -D /data/cluster/new/ -E UTF8

// Stop the standby nodes
docker-compose stop keeper1
docker-compose stop keeper0

// upgrade the primary
supervisorctl stop stolon-keeper
cd /tmp

// Copy the previous conf, to preserve
mkdir /tmp/conf/
cp /data/cluster/postgres/pg_hba.conf /data/cluster/postgres/postgresql.conf /data/cluster/postgres/recovery.conf /tmp/conf/

// consistency check 
/usr/lib/postgresql/14/bin/pg_upgrade -b /usr/lib/postgresql/11/bin -B /usr/lib/postgresql/14/bin -d /data/cluster/postgres -D /data/cluster/new -c

// upgrading the data directories with hard links 
/usr/lib/postgresql/14/bin/pg_upgrade -b /usr/lib/postgresql/11/bin -B /usr/lib/postgresql/14/bin -d /data/cluster/postgres -D /data/cluster/new -k


cp /tmp/conf/* /data/cluster/new

// Point the node to the new directory / replace existing
rm -rf /data/cluster/postgres/
mv /data/cluster/new /data/cluster/postgres

// Restart the keeper with new version of postgres
// edit supervisord.conf to change pg version from 11 to 14
supervisorctl reread
supervisorctl update

// start the rest of the nodes
docker-compose start keeper1
docker-compose start keeper0
```


# rsync
Currently Replicas delete the data directory and take a pg_basebackup regardless of rsync as the major upgrade destroyts replication slots and assigns new UIDs to all nodes in the cluster.

Rsync command
```
rsync --archive --delete --hard-links --size-only --no-inc-recursive --human-readable --progress /data/cluster/postgres /data/cluster/new keeper0:/data/cluster
```
Also the directory structure on standby should be the same so run below on standby
```
mkdir /data/cluster/new
chown -R postgres:postgres /data/cluster/new
```

To keep the container running but stopping standby keepers run:
```
docker-compose exec keeper0 supervisorctl stop stolon-keeper
docker-compose exec keeper1 supervisorctl stop stolon-keeper
```
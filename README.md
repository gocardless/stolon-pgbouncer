stolon-pgbouncer
================

Tests
-----

Run unit tests:
    make test

Run acceptance tests:
    make test-acceptance
This will spin up a cluster using docker-compose and run acceptance tests against it.
Right now there's a race between the cluster becoming stable and the test starting, so it
may be a bit flaky.

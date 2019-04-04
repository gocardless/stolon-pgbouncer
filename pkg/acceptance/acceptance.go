package acceptance

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/coreos/etcd/clientv3"
	kitlog "github.com/go-kit/kit/log"
	. "github.com/gocardless/stolon-pgbouncer/pkg/acceptance/matchers"
	"github.com/gocardless/stolon-pgbouncer/pkg/stolon"
	"github.com/jackc/pgx"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

// docker-compose exposes these ports on our host machine. This allows us to run our test
// from the machine that is running docker, rather than from within the docker containers
// themselves.
var (
	pgBouncerPorts = map[string]string{
		"pgbouncer": "6432",
		"keeper0":   "6433",
		"keeper1":   "6434",
		"keeper2":   "6435",
	}
)

func RunAcceptance(ctx context.Context, logger kitlog.Logger) {
	// Repeatedly attempt to connect to PgBouncer on the given port, timing out after a
	// given limit.
	pgConnect := func(port string) *pgx.Conn {
		var conn *pgx.Conn

		defer func(begin time.Time) {
			logger.Log("event", "pg.connect", "msg", "connected to PostgreSQL via PgBouncer",
				"elapsed", time.Since(begin).Seconds())
		}(time.Now())

		Eventually(
			func() error {
				logger.Log("event", "pg.connect.poll", "msg", "attempting to connect to PostgreSQL via PgBouncer")
				cfg, err := pgx.ParseConnectionString(
					fmt.Sprintf(
						"user=postgres dbname=postgres host=localhost port=%s "+
							"connect_timeout=1 sslmode=disable",
						port,
					),
				)

				Expect(err).NotTo(HaveOccurred())
				conn, err = pgx.Connect(cfg)
				return err
			},
			time.Minute,
			time.Second,
		).Should(
			Succeed(),
		)

		return conn
	}

	// PgBouncer Container
	conn := pgConnect(pgBouncerPorts["pgbouncer"])

	// Etcd client
	client := mustStore()

	// Given a database connection, attempt to query the inet_server_addr, which can be used
	// to identify which machine we're talking to. This is necessary to identify whether
	// PgBouncer has routed our connection correctly.
	inetServerAddr := func(conn *pgx.Conn) string {
		rows, err := conn.Query(`SELECT inet_server_addr();`)
		Expect(err).NotTo(HaveOccurred())

		defer rows.Close()

		var addr sql.NullString

		Expect(rows.Next()).To(BeTrue())
		Expect(rows.Scan(&addr)).To(Succeed())

		// Remove any network suffix from the IP (e.g., 172.17.0.3/32)
		return strings.SplitN(addr.String, "/", 2)[0]
	}

	// Given an etcd client, fetch the stolon cluster data
	getClusterData := func(client *clientv3.Client) *stolon.Clusterdata {
		clusterdata, err := stolon.GetClusterdata(ctx, client, "stolon/cluster/main/clusterdata")
		Expect(err).NotTo(HaveOccurred())

		return clusterdata
	}

	expectPgbouncerPointsToMaster := func(clusterdata *stolon.Clusterdata) string {
		masterAddress := clusterdata.Master().Status.ListenAddress

		logger.Log("expect", "the PgBouncer container proxies to the master PostgreSQL", "masterAddress", masterAddress)
		connectedAddress := inetServerAddr(conn)
		Expect(connectedAddress).To(Equal(masterAddress))

		for host, port := range pgBouncerPorts {
			conn := pgConnect(port)
			connectedAddr := inetServerAddr(conn)
			logger.Log("expect", "the PgBouncer on keeper to proxy to the master PostgreSQL", "keeper", host, "masterAddress", masterAddress)
			Expect(connectedAddr).To(Equal(masterAddress))
		}

		return masterAddress
	}

	getKeeperHealthStatus := func(dbs []stolon.DB) []bool {
		statuses := []bool{}
		for _, db := range dbs {
			logger.Log("msg", "keeper status", "keeper", db.Spec.KeeperUID, "status", db.Status.Healthy)
			statuses = append(statuses, db.Status.Healthy)
		}
		return statuses
	}

	logClusterStatus := func() {
		clusterData := getClusterData(client)
		syncStandbys := []string{}
		for _, s := range clusterData.SynchronousStandbys() {
			syncStandbys = append(syncStandbys, s.Spec.KeeperUID)
		}
		logger.Log("msg", "cluster status", "master", clusterData.Master().Spec.KeeperUID, "synchronous_standbys", strings.Join(syncStandbys, ","))
	}

	execCommand := func(ctx context.Context, command string, args ...string) error {
		cmd := exec.CommandContext(ctx, command, args...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	runFailover := func() error {
		logger.Log("msg", "running failover")
		return execCommand(ctx, "docker-compose", "exec", "pgbouncer", "/stolon-pgbouncer/bin/stolon-pgbouncer.linux_amd64", "failover")
	}

	Describe("stolon-pgbouncer", func() {
		Specify("Failover", func() {
			logger.Log("msg", "checking that all keepers are healthy before running failover")

			Eventually(func() []bool {
				return getKeeperHealthStatus(getClusterData(client).Databases())
			}).Should(All(Equal(true)))

			logClusterStatus()

			oldMaster := expectPgbouncerPointsToMaster(getClusterData(client))

			err := runFailover()
			Expect(err).NotTo(HaveOccurred())

			newMaster := expectPgbouncerPointsToMaster(getClusterData(client))
			Expect(newMaster).NotTo(Equal(oldMaster))

			logClusterStatus()
		})

		Specify("Failover with open transaction", func() {
			logger.Log("msg", "start transaction, preventing PgBouncer pause")
			txact, err := conn.Begin()
			Expect(err).NotTo(HaveOccurred())

			oldMaster := expectPgbouncerPointsToMaster(getClusterData(client))

			logger.Log("msg", "checking that all keepers are healthy before running failover")
			Eventually(func() []bool {
				return getKeeperHealthStatus(getClusterData(client).Databases())
			}).Should(All(Equal(true)))

			logger.Log("msg", "this failover should fail, due to the PgBouncer pause expiry")
			err = runFailover()
			Expect(err).To(HaveOccurred())

			newMaster := expectPgbouncerPointsToMaster(getClusterData(client))
			Expect(newMaster).To(Equal(oldMaster))

			logger.Log("msg", "rollback transaction, allowing PgBouncer pause")
			Expect(txact.Rollback()).To(Succeed())

			logClusterStatus()
		})

		Specify("Failover with failed asynchronous standbys", func() {
			oldMaster := expectPgbouncerPointsToMaster(getClusterData(client))

			for _, db := range getClusterData(client).AsynchronousStandbys() {
				logger.Log("msg", "pausing keeper", "keeper", db.Spec.KeeperUID)
				err := execCommand(ctx, "docker-compose", "pause", db.Spec.KeeperUID)
				Expect(err).NotTo(HaveOccurred())
			}

			logger.Log("msg", "checking that the async keepers are all unhealthy before running failover")
			Eventually(func() []bool {
				return getKeeperHealthStatus(getClusterData(client).AsynchronousStandbys())
			},
				time.Minute,
				time.Second,
			).Should(All(Equal(false)))

			logger.Log("msg", "this failover should fail, due to the PgBouncer pause expiry")
			err := runFailover()
			Expect(err).To(HaveOccurred())

			for _, db := range getClusterData(client).AsynchronousStandbys() {
				logger.Log("msg", "unpausing keeper", "keeper", db.Spec.KeeperUID)
				err := execCommand(ctx, "docker-compose", "unpause", db.Spec.KeeperUID)
				Expect(err).NotTo(HaveOccurred())
			}

			newMaster := expectPgbouncerPointsToMaster(getClusterData(client))
			Expect(newMaster).To(Equal(oldMaster))
		})
	})
}

func mustStore() *clientv3.Client {
	client, err := clientv3.New(
		clientv3.Config{
			Endpoints:            []string{"localhost:2379"},
			DialTimeout:          3 * time.Second,
			DialKeepAliveTime:    30 * time.Second,
			DialKeepAliveTimeout: 5 * time.Second,
		},
	)

	Expect(err).NotTo(HaveOccurred())

	return client
}

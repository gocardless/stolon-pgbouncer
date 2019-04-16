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
	"github.com/gocardless/stolon-pgbouncer/pkg/stolon"
	"github.com/jackc/pgx"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

// docker-compose exposes these ports on our host machine. This allows us to run our test
// from the machine that is running docker, rather than from within the docker containers
// themselves.
var (
	binary         = "/stolon-pgbouncer/bin/stolon-pgbouncer.linux_amd64"
	pgBouncerPorts = map[string]string{
		"pgbouncer": "6432",
		"keeper0":   "6433",
		"keeper1":   "6434",
		"keeper2":   "6435",
	}
)

func RunAcceptance(ctx context.Context, logger kitlog.Logger) {
	Describe("stolon-pgbouncer", func() {
		Describe("Health check", func() {
			var (
				err   error
				token string
			)

			JustBeforeEach(func() {
				err = execCommand(ctx, "docker-compose", "exec", "pgbouncer", binary, "failover", "--health-check-only", "--token", token)
			})

			BeforeEach(func() { token = "failover-token" })

			It("Successfully health checks", func() {
				Expect(err).NotTo(HaveOccurred(), "expected successful health check")
			})

			Context("With invalid token", func() {
				BeforeEach(func() { token = "invalid-token" })

				It("Fails health check", func() {
					Expect(err).To(HaveOccurred(), "health check should not pass with invalid token")
				})
			})

			Context("With an unresponsive endpoint", func() {
				var (
					targetKeeper = "keeper0"
				)

				BeforeEach(func() {
					Expect(
						execCommand(ctx, "docker-compose", "pause", targetKeeper),
					).To(Succeed(), "pausing %s to succeed", targetKeeper)
				})

				AfterEach(func() {
					Expect(
						execCommand(ctx, "docker-compose", "unpause", targetKeeper),
					).To(Succeed(), "unpausing %s to succeed", targetKeeper)
				})

				It("Fails the health check", func() {
					Expect(err).To(HaveOccurred(), "health check should fail due to unresponsive keeper")
				})
			})
		})

		Describe("Failover", func() {
			var (
				err    error
				client *clientv3.Client
			)

			JustBeforeEach(func() {
				err = execCommand(ctx, "docker-compose", "exec", "pgbouncer", binary, "failover", "--pause-expiry", "50s")
			})

			BeforeEach(func() {
				client = mustStore()

				logger.Log("msg", "checking that all keepers are healthy before running failover")
				Eventually(func() error { return mustClusterdata(ctx, client).CheckHealthy() }).Should(
					Succeed(), "timed out waiting for all keepers to become healthy",
				)
			})

			AfterEach(func() {
				logger.Log("msg", "verifying all PgBouncers point at master")
				masterAddress := mustClusterdata(ctx, client).Master().Status.ListenAddress
				for host, port := range pgBouncerPorts {
					addr := inetServerAddr(pgConnect(logger, port))
					Expect(addr).To(Equal(masterAddress), "PgBouncer on %s connect to master Postgres", host)
				}
			})

			It("Successfully fails over to new master", func() {
				Expect(err).NotTo(HaveOccurred(), "failover to be successful")
			})

			Context("With open transaction", func() {
				var (
					conn  *pgx.Conn
					txact *pgx.Tx
				)

				BeforeEach(func() {
					conn = pgConnect(logger, pgBouncerPorts["pgbouncer"])

					var txerr error
					txact, txerr = conn.Begin()
					Expect(txerr).NotTo(HaveOccurred(), "begin transaction")
				})

				It("Fails to failover without interrupting connection", func() {
					Expect(err).To(HaveOccurred(), "failover to have failed due to open transaction")
					Expect(txact.Rollback()).To(Succeed(), "transaction to rollback, as connection should have been uninterrupted")
				})
			})
		})
	})
}

// execCommand spawns a new process inheriting our current IO FDs
func execCommand(ctx context.Context, command string, args ...string) error {
	cmd := exec.CommandContext(ctx, command, args...)

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// inetServerAddr queries the given database connection for inet_server_addr, which can be
// used to identify which server we're executing queries on. If we're connect to a
// PgBouncer, the result will be the server we're proxied to.
func inetServerAddr(conn *pgx.Conn) string {
	rows, err := conn.Query(`SELECT inet_server_addr();`)
	Expect(err).NotTo(HaveOccurred())

	defer rows.Close()

	var addr sql.NullString

	Expect(rows.Next()).To(BeTrue())
	Expect(rows.Scan(&addr)).To(Succeed())

	// Remove any network suffix from the IP (e.g., 172.17.0.3/32)
	return strings.SplitN(addr.String, "/", 2)[0]
}

// pgConnect repeatedly attempts to connect to Postgres on the given port, timing out
// after a given limit.
func pgConnect(logger kitlog.Logger, port string) *pgx.Conn {
	var conn *pgx.Conn

	defer func(begin time.Time) {
		logger.Log("event", "postgres_connect", "msg", "connected to PostgreSQL via PgBouncer",
			"duration", time.Since(begin).Seconds())
	}(time.Now())

	Eventually(
		func() error {
			logger.Log("event", "postgres_poll", "msg", "attempting to connect to PostgreSQL via PgBouncer")
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
		Succeed(), "connect to Postgres via a PgBouncer",
	)

	return conn
}

// mustClusterdata returns the stolon cluster data stored in the provided etcd client
func mustClusterdata(ctx context.Context, client *clientv3.Client) *stolon.Clusterdata {
	clusterdata, err := stolon.GetClusterdata(ctx, client, "stolon/cluster/main/clusterdata")
	Expect(err).NotTo(HaveOccurred())

	return clusterdata
}

// mustStore returns an etcd client connection to our store
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

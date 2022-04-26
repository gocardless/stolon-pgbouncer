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
	"github.com/gocardless/stolon-pgbouncer/pkg/pgbouncer"
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
		var (
			client *clientv3.Client
		)

		BeforeEach(func() {
			client = mustStore()

			logger.Log("msg", "checking cluster is healthy before starting test")
			Eventually(func() error { return mustClusterdata(ctx, client).CheckHealthy(1) }).Should(
				Succeed(), "timed out waiting for all keepers to become healthy",
			)

			// Wait for all PgBouncers to update before we begin
			expectPgBouncersPointToMaster(ctx, logger, client)
		})

		Describe("Failover", func() {
			var (
				err error
			)

			JustBeforeEach(func() {
				err = execCommand(ctx, "docker-compose", "exec", "pgbouncer", binary, "failover", "--pause-expiry", "20s")
			})

			// Confirm PgBouncers have settled before marking test as success
			AfterEach(func() { expectPgBouncersPointToMaster(ctx, logger, client) })

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

			Context("With unhealthy keeper", func() {
				var (
					asyncKeeper string
				)

				BeforeEach(func() {
					asyncKeeper = mustClusterdata(ctx, client).AsynchronousStandbys()[0].Spec.KeeperUID
					Expect(
						execCommand(ctx, "docker-compose", "exec", asyncKeeper, "pkill", "-STOP", "stolon-keeper"),
					).To(Succeed(), "stopping %s stolon-keeper service", asyncKeeper)

					Eventually(func() error { return mustClusterdata(ctx, client).CheckHealthy(1) }).Should(
						Not(Succeed()), "timed out waiting for > min keepers to become unhealthy",
					)
				})

				AfterEach(func() {
					Expect(
						execCommand(ctx, "docker-compose", "exec", asyncKeeper, "pkill", "-CONT", "stolon-keeper"),
					).To(Succeed(), "starting %s stolon-keeper service", asyncKeeper)
				})

				It("Fails to failover", func() {
					Expect(err).To(HaveOccurred(), "failover succeeded when it should have failed due to unhealthy keeper")
				})

			})
		})

		// Placing the health check after the failover means we can rely on our docker
		// services having been fully booted in CI.
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

		Describe("Pauser", func() {
			Context("When PgBouncer is paused and pauser reboots", func() {
				var (
					conn    *pgx.Conn
					bouncer *pgbouncer.PgBouncer
					keeper  = "keeper0"
				)

				BeforeEach(func() {
					conn = pgConnect(logger, pgBouncerPorts["keeper0"])
					bouncer = &pgbouncer.PgBouncer{
						Executor: &pgbouncer.AuthorizedExecutor{
							User:      "stolon",
							Password:  "stolonpass",
							Database:  "pgbouncer",
							SocketDir: "localhost",
							Port:      pgBouncerPorts[keeper],
						},
					}

					err := bouncer.Pause(ctx)
					Expect(err).NotTo(HaveOccurred(), "failed to pause %s PgBouncer", keeper)
				})

				AfterEach(func() {
					err := bouncer.Resume(ctx)
					Expect(err).NotTo(HaveOccurred(), "failed to resume %s PgBouncer", keeper)
				})

				// The pauser will issue a PgBouncer resume whenever it boots as a precaution to
				// catch any dangling pause commands it may have run as the previous process, like
				// if the process was violently killed mid-migration.
				//
				// We can rely on the docker supervisord process bringing the pauser back up if we
				// violently kill it, so it's sufficient to check that the reboot enables us to
				// query the database, as a lack of resume would make our select now() timeout.
				It("Resumes PgBouncer", func() {
					err := execCommand(ctx, "docker-compose", "exec", keeper, "pkill", "-9", "--full", "pauser")
					Expect(err).NotTo(HaveOccurred(), "killing %s pauser service", keeper)

					queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
					defer cancel()

					_, err = conn.ExecEx(queryCtx, `select now();`, nil)
					Expect(err).NotTo(
						HaveOccurred(), "failed to execute query via PgBouncer",
					)
				})
			})
		})
	})
}

func expectPgBouncersPointToMaster(ctx context.Context, logger kitlog.Logger, client *clientv3.Client) {
	logger.Log("msg", "expect all PgBouncers point at master")
	masterAddress := mustClusterdata(ctx, client).Master().Status.ListenAddress
	for host, port := range pgBouncerPorts {
		addr := inetServerAddr(pgConnect(logger, port))
		Expect(addr).To(Equal(masterAddress), "PgBouncer on %s connect to master Postgres", host)
	}
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
					"user=stolon dbname=postgres password=stolonpass host=localhost port=%s "+
						"connect_timeout=1 sslmode=disable",
					port,
				),
			)

			Expect(err).NotTo(HaveOccurred())
			conn, err = pgx.Connect(cfg)
			return err
		},
	).Should(
		Succeed(), "connect to Postgres via a PgBouncer",
	)

	return conn
}

// mustClusterdata returns the stolon cluster data stored in the provided etcd client
func mustClusterdata(ctx context.Context, client *clientv3.Client) *stolon.Clusterdata {
	var cd *stolon.Clusterdata

	Eventually(
		func() (err error) {
			cd, err = stolon.GetClusterdata(ctx, client, "stolon/cluster/main/clusterdata")

			return
		},
	).Should(
		Succeed(), "timed out trying to retrieve clusterdata",
	)

	return cd
}

// mustStore returns an etcd client connection to our store
func mustStore() *clientv3.Client {
	var client *clientv3.Client

	Eventually(
		func() (err error) {
			client, err = clientv3.New(
				clientv3.Config{
					Endpoints:            []string{"localhost:2379"},
					DialTimeout:          3 * time.Second,
					DialKeepAliveTime:    30 * time.Second,
					DialKeepAliveTimeout: 5 * time.Second,
				},
			)

			return
		},
	).Should(
		Succeed(), "connection to etcd could not be established",
	)

	return client
}

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
	"github.com/jackc/pgx"
	. "github.com/onsi/gomega"

	"github.com/gocardless/stolon-pgbouncer/pkg/stolon"
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
	getClusterdata := func(client *clientv3.Client) *stolon.Clusterdata {
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

	getKeeperHealthStatus := func(client *clientv3.Client) []bool {
		clusterData := getClusterdata(client)
		statuses := make([]bool, 0)

		for _, db := range clusterData.Dbs {
			statuses = append(statuses, db.Status.Healthy)
		}
		return statuses
	}

	logClusterStatus := func() {
		clusterData := getClusterdata(client)
		syncStandbys := []string{}
		for _, s := range clusterData.SynchronousStandbys() {
			syncStandbys = append(syncStandbys, s.Spec.KeeperUID)
		}
		logger.Log("msg", "cluster status", "master", clusterData.Master().Spec.KeeperUID, "synchronous_standbys", strings.Join(syncStandbys, ","))
	}

	runFailover := func() {
		logger.Log("msg", "running failover")
		cmd := exec.CommandContext(ctx, "docker-compose", "exec", "pgbouncer", "/stolon-pgbouncer/bin/stolon-pgbouncer.linux_amd64", "failover", "--pause-expiry=1m")
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err := cmd.Run()
		Expect(err).NotTo(HaveOccurred())
	}

	logger.Log("msg", "checking that all keepers are healthy before running failover")
	Eventually(getKeeperHealthStatus(client)).Should(Equal([]bool{true, true, true}))

	logClusterStatus()

	oldMaster := expectPgbouncerPointsToMaster(getClusterdata(client))

	runFailover()

	newMaster := expectPgbouncerPointsToMaster(getClusterdata(client))
	Expect(newMaster).NotTo(Equal(oldMaster))

	logClusterStatus()
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

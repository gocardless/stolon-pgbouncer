package acceptance

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/alecthomas/kingpin"
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

func RunAcceptance(ctx context.Context, logger kitlog.Logger) { //, opt AcceptanceOptions) {
	// TODO: Is this comment correct? This function doesn't necessarily connect to the
	// PgBouncer that goes through the master.
	//
	// Attempt a connection to PgBouncer which connects through to the master
	// PostgreSQL database.
	// Connection flow:
	// PgBouncer on PgBouncer container -> PgBouncer on master keeper node ->
	// Stolon Proxy -> PostgreSQL
	pgTryConnect := func(host, port string) (*pgx.Conn, error) {
		cfg, err := pgx.ParseConnectionString(
			fmt.Sprintf(
				"user=postgres dbname=postgres host=%s port=%s "+
					"connect_timeout=1 sslmode=disable",
				host,
				port,
			),
		)

		Expect(err).NotTo(HaveOccurred())
		return pgx.Connect(cfg)
	}

	// Repeatedly attempt to connect to PgBouncer proxied PostgreSQL, timing out after a
	// given limit.
	pgConnect := func(port string) (conn *pgx.Conn) {
		defer func(begin time.Time) {
			logger.Log("event", "pg.connect", "msg", "connected to PostgreSQL via PgBouncer",
				"elapsed", time.Since(begin).Seconds())
		}(time.Now())

		Eventually(
			func() (err error) { conn, err = pgTryConnect("localhost", port); return },
			time.Minute,
			time.Second,
		).Should(
			Succeed(),
		)

		return conn
	}

	// PgBouncer Container
	conn := pgConnect(pgBouncerPorts["pgbouncer"])

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

	// Get cluster data
	client := mustStore()

	getClusterdata := func(client *clientv3.Client) *stolon.Clusterdata {
		clusterdata, err := stolon.GetClusterdata(ctx, client, "stolon/cluster/main/clusterdata")
		Expect(err).NotTo(HaveOccurred())

		return clusterdata
	}

	expectPgbouncerPointToMaster := func(clusterdata *stolon.Clusterdata) string {
		// Get current master IP address
		master := clusterdata.Master()
		masterAddress := master.Status.ListenAddress

		logger.Log("expect", "PgBouncer container to proxy to master PostgreSQL", "masterAddress", masterAddress)
		connectedAddress := inetServerAddr(conn)
		Expect(connectedAddress).To(
			Equal(masterAddress),
		)

		for host, port := range pgBouncerPorts {
			conn := pgConnect(port)
			connectedAddr := inetServerAddr(conn)
			logger.Log("expect", "PgBouncer on keeper to proxy to master PostgreSQL", "keeper", host, "masterAddress", masterAddress)
			Expect(connectedAddr).To(
				Equal(masterAddress),
			)
		}

		return masterAddress
	}

	oldMaster := expectPgbouncerPointToMaster(getClusterdata(client))

	logger.Log("msg", "running failover")

	cmd := exec.CommandContext(ctx, "docker-compose", "exec", "pgbouncer", "/stolon-pgbouncer/bin/stolon-pgbouncer.linux_amd64", "failover", "--pause-expiry=1m")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()

	Expect(err).NotTo(HaveOccurred())

	Expect(expectPgbouncerPointToMaster(getClusterdata(client))).NotTo(
		Equal(oldMaster),
	)
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

	if err != nil {
		kingpin.Fatalf("failed to connect to etcd: %s", err)
	}

	return client
}

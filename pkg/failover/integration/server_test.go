package integration

import (
	"context"
	"time"

	kitlog "github.com/go-kit/kit/log"
	"github.com/gocardless/stolon-pgbouncer/pkg/failover"
	"github.com/gocardless/stolon-pgbouncer/pkg/pgbouncer"
	"github.com/gocardless/stolon-pgbouncer/pkg/pgbouncer/integration"
	"github.com/jackc/pgx"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Server", func() {
	var (
		ctx     context.Context
		logger  = kitlog.NewLogfmtLogger(GinkgoWriter)
		cancel  func()
		bouncer *pgbouncer.PgBouncer
		cleanup func()
		server  *failover.Server

		// Integration Postgres database
		database, host, user, password, port = integration.PostgresEnv()
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
		bouncer, cleanup = integration.StartPgBouncer(database, user, password, port, "transaction")
		server = failover.NewServer(logger, bouncer)

		// Point the PgBouncer configuration at our integration Postgres database
		Expect(bouncer.GenerateConfig(host)).To(Succeed())
		Expect(bouncer.Reload(ctx)).To(Succeed())
	})

	AfterEach(func() {
		cleanup()
		cancel()
	})

	connectToDatabase := func() *pgx.Conn {
		executor := bouncer.Executor.(pgbouncer.AuthorizedExecutor)
		conn, err := pgx.Connect(
			pgx.ConnConfig{
				Host:     executor.SocketDir,
				Port:     6432,
				Database: database,
				User:     user,
			},
		)

		Expect(err).NotTo(HaveOccurred())
		return conn
	}

	Describe("Pause", func() {
		var (
			timeout = int64(250 * time.Millisecond)
			expiry  = int64(2 * time.Second)
		)

		// TODO: This is a pretty crappy test. When we have a proper acceptance flow that
		// verifies the failover sad-path we can look to get rid of this.
		It("Causes queries to block until expiry", func() {
			conn := connectToDatabase()
			defer conn.Close()

			// Confirm we can execute queries
			Expect(conn.ExecEx(ctx, "select now()", nil)).NotTo(BeNil())

			// Pause the pools, causing all new connections to block. Set a 3s expiry, at which
			// point we expect to resume the database.
			_, err := server.Pause(ctx, &failover.PauseRequest{Timeout: timeout, Expiry: expiry})
			Expect(err).NotTo(HaveOccurred(), "failed to pause PgBouncer")

			queryStartAt := time.Now()

			// We expect this will eventually succeed, as the pause expires after 3s
			_, err = conn.ExecEx(ctx, "select now()", nil)
			Expect(err).NotTo(HaveOccurred())

			// Expect our query to have taken approximately the expiry time
			Expect(time.Now().Sub(queryStartAt)).Should(
				BeNumerically("~", expiry, 200*time.Millisecond),
			)
		})
	})
})

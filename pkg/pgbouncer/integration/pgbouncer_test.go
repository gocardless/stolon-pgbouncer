package integration

import (
	"context"
	"io/ioutil"
	"path"
	"time"

	"github.com/gocardless/stolon-pgbouncer/pkg/pgbouncer"
	"github.com/jackc/pgx"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("PgBouncer", func() {
	var (
		ctx     context.Context
		cancel  func()
		bouncer *pgbouncer.PgBouncer
		cleanup func()

		// Integration Postgres database
		database, host, user, password, port = PostgresEnv()
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
		bouncer, cleanup = StartPgBouncer(database, user, password, port, "session")
	})

	AfterEach(func() {
		cleanup()
		cancel()
	})

	readlogs := func() string {
		workspace := path.Dir(bouncer.ConfigFile)
		logs, err := ioutil.ReadFile(path.Join(workspace, "pgbouncer.log"))

		Expect(err).NotTo(HaveOccurred())
		return string(logs)
	}

	connectToDatabase := func() (*pgx.Conn, error) {
		executor := bouncer.Executor.(pgbouncer.AuthorizedExecutor)
		return pgx.Connect(
			pgx.ConnConfig{
				Host:     executor.SocketDir,
				Port:     6432,
				Database: database,
				User:     user,
			},
		)
	}

	mustConnectToDatabase := func() *pgx.Conn {
		conn, err := connectToDatabase()

		Expect(err).NotTo(HaveOccurred())
		return conn
	}

	Context("Pointed at the integration database", func() {
		BeforeEach(func() {
			// Point the PgBouncer configuration at our integration Postgres database
			Expect(bouncer.GenerateConfig(host)).To(Succeed())
			Expect(bouncer.Reload(ctx)).To(Succeed())
		})

		Describe("ShowDatabases", func() {
			It("Correctly parses databases", func() {
				// Create a connection so we can validate the CurrentConnections value
				conn := mustConnectToDatabase()
				defer conn.Close()

				databases, err := bouncer.ShowDatabases(ctx)

				Expect(err).NotTo(HaveOccurred())
				Expect(databases).To(ConsistOf(
					pgbouncer.Database{
						Name: "pgbouncer",
						Port: "6432",
					},
					pgbouncer.Database{
						Name: database,
						Port: port,
						Host: host,

						// We expect to see a single connection from the one we created at the start of
						// our test.
						CurrentConnections: 1,
					},
				))
			})
		})

		Describe("Disable", func() {
			It("Prevents new client connections", func() {
				// Create a connection prior to the disable so we can check the bahviour
				// post-operation
				original := mustConnectToDatabase()
				defer original.Close()

				Expect(bouncer.Disable(ctx)).To(Succeed())

				// This connection should never succeed, as we've asked to disable new attempts
				_, err := connectToDatabase()
				Expect(err).To(BeAssignableToTypeOf(pgx.PgError{}))

				if err, ok := err.(pgx.PgError); ok {
					Expect(err.Code).To(Equal(pgbouncer.PoolerError))
					Expect(err.Message).To(ContainSubstring("database does not allow connections"))
				}

				// Our original connection should still be functional, as disable affects only new
				// connections
				Expect(original.ExecEx(ctx, "select now()", nil)).NotTo(BeNil())
			})
		})
	})

	Describe("Reload", func() {
		It("Succeeds, triggering config reload", func() {
			Expect(bouncer.ShowDatabases(ctx)).To(
				ContainElement(
					pgbouncer.Database{
						Name: database,
						Host: "{{.Host}}",
						Port: port,
					},
				),
			)

			Expect(bouncer.GenerateConfig("new-host")).To(Succeed())
			Expect(bouncer.Reload(ctx)).To(Succeed())
			Eventually(readlogs).Should(ContainSubstring("LOG RELOAD command issued"))

			Expect(bouncer.ShowDatabases(ctx)).To(
				ContainElement(
					pgbouncer.Database{
						Name: database,
						Host: "new-host",
						Port: port,
					},
				),
			)
		})
	})

	Describe("Pause", func() {
		Context("When not already paused", func() {
			It("Succeeds", func() {
				Expect(bouncer.Pause(ctx)).To(Succeed())
				Eventually(readlogs).Should(ContainSubstring("LOG PAUSE command issued"))
			})
		})

		Context("When session is blocking pause", func() {
			It("Times out and resumes", func() {
				// Point the PgBouncer configuration at our integration Postgres database
				Expect(bouncer.GenerateConfig(host)).To(Succeed())
				Expect(bouncer.Reload(ctx)).To(Succeed())

				conn := mustConnectToDatabase()
				defer conn.Close()

				// Establish a session pooled connection, which should prevent PgBouncer from
				// being able to pause the pool.
				Expect(conn.ExecEx(ctx, "select now()", nil)).NotTo(BeNil())

				timeoutCtx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
				defer cancel()

				// We can expect this pause command not to succeed, as it will timeout
				Expect(bouncer.Pause(timeoutCtx)).To(MatchError("context deadline exceeded"))

				// New commands that come to the database
				anotherConn := mustConnectToDatabase()
				defer anotherConn.Close()

				Expect(conn.ExecEx(ctx, "select now()", nil)).NotTo(BeNil())
			})
		})

		Context("When already paused", func() {
			It("Succeeds", func() {
				Expect(bouncer.Pause(ctx)).To(Succeed())
				Expect(bouncer.Pause(ctx)).To(Succeed())

				Eventually(readlogs).Should(ContainSubstring("LOG PAUSE command issued"))
				Eventually(readlogs).Should(ContainSubstring("ERROR already suspended/paused"))
			})
		})
	})

	Describe("Resume", func() {
		Context("When paused", func() {
			BeforeEach(func() { Expect(bouncer.Pause(ctx)).To(Succeed()) })

			It("Succeeds", func() {
				Expect(bouncer.Resume(ctx)).To(Succeed())
				Eventually(readlogs).Should(ContainSubstring("LOG RESUME command issued"))
			})
		})

		Context("When not paused", func() {
			It("Succeeds", func() {
				Expect(bouncer.Resume(ctx)).To(Succeed())
				Eventually(readlogs).Should(ContainSubstring("ERROR pooler is not paused/suspended"))
			})
		})
	})
})

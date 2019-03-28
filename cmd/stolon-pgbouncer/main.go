package main

import (
	"context"
	"encoding/json"
	"fmt"
	stdlog "log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"

	"github.com/alecthomas/kingpin"
	"github.com/coreos/etcd/clientv3"
	"github.com/coreos/etcd/mvcc/mvccpb"
	kitlog "github.com/go-kit/kit/log"
	level "github.com/go-kit/kit/log/level"
	"github.com/gocardless/pgsql-cluster-manager/pkg/etcd"
	"github.com/gocardless/pgsql-cluster-manager/pkg/pgbouncer"
	"github.com/gocardless/pgsql-cluster-manager/pkg/streams"
	"github.com/oklog/run"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var logger kitlog.Logger

var (
	app = kingpin.New("stolon-pgbouncer", "Tooling to manage PgBouncer with a stolon Postgres cluster").Version(versionStanza())

	// Global flags applying to every command
	debug          = app.Flag("debug", "Enable debug logging").Default("false").Bool()
	metricsAddress = app.Flag("metrics-address", "Address to bind HTTP metrics listener").Default("127.0.0.1").String()
	metricsPort    = app.Flag("metrics-port", "Port to bind HTTP metrics listener").Default("9446").Uint16()

	// Stolon compatible flags
	clusterName           = app.Flag("cluster-name", "Name of the stolon cluster").Required().String()
	storeBackend          = app.Flag("store-backend", "Store backend provider").Default("etcdv3").String()
	storePrefix           = app.Flag("store-prefix", "Store prefix").Default("stolon/cluster").String()
	storeEndpoints        = app.Flag("store-endpoints", "Comma delimited list of store endpoints").Default("http://127.0.0.1:2379").String()
	storeTimeout          = app.Flag("store-timeout", "Timeout for store operations").Default("3s").Duration()
	storeDialTimeout      = app.Flag("store-dial-timeout", "Timeout when connecting to store").Default("3s").Duration()
	storeKeepaliveTime    = app.Flag("store-keepalive-time", "Time after which client pings server to check transport").Default("30s").Duration()
	storeKeepaliveTimeout = app.Flag("store-keepalive-timeout", "Timeout for store keepalive probe").Default("5s").Duration()

	supervise                   = app.Command("supervise", "Manages local PgBouncer")
	superviseExecPgBouncer      = supervise.Flag("exec-pgbouncer", "stolon-pgbouncer will run PgBouncer as a child process").Default("false").Bool()
	supervisePollInterval       = supervise.Flag("poll-interval", "Store poll interval").Default("1m").Duration()
	superviseUser               = supervise.Flag("pgbouncer-user", "Admin user of PgBouncer").Default("pgbouncer").String()
	supervisePassword           = supervise.Flag("pgbouncer-password", "Password for admin user").Default("").String()
	superviseDatabase           = supervise.Flag("pgbouncer-database", "PgBouncer special database (inadvisable to change)").Default("pgbouncer").String()
	superviseSocketDir          = supervise.Flag("pgbouncer-socket-dir", "Directory in which the unix socket resides").Default("/var/run/postgresql").String()
	supervisePort               = supervise.Flag("pgbouncer-port", "Directory in which the unix socket resides").Default("6432").String()
	superviseConfigFile         = supervise.Flag("pgbouncer-config-file", "Path to PgBouncer config file").Default("/etc/pgbouncer/pgbouncer.ini").String()
	superviseConfigTemplateFile = supervise.Flag("pgbouncer-config-template-file", "Path to PgBouncer config template file").Default("/etc/pgbouncer/pgbouncer.ini.template").String()
	supervisePgBouncerTimeout   = supervise.Flag("pgbouncer-timeout", "Timeout for PgBouncer operations").Default("5s").Duration()
	superviseRetryTimeout       = supervise.Flag("pgbouncer-retry-timeout", "Retry failed PgBouncer operations at this interval").Default("5s").Duration()

	failover = app.Command("failover", "Run a zero-downtime failover of the Postgres primary")
)

// Clusterdata is a minimal extraction that we need from stolon
type Clusterdata struct {
	Proxy struct {
		Spec struct {
			MasterDbUID string `json:"masterDbUid"`
		} `json:"spec"`
	} `json:"proxy"`

	Dbs map[string]struct {
		Status struct {
			ListenAddress string `json:"listenAddress"`
			Port          string `json:"port"`
		} `json:"status"`
	} `json:"dbs"`
}

func main() {
	command := kingpin.MustParse(app.Parse(os.Args[1:]))

	logger = kitlog.NewLogfmtLogger(kitlog.NewSyncWriter(os.Stderr))
	logger = kitlog.With(logger, "ts", kitlog.DefaultTimestampUTC, "caller", kitlog.DefaultCaller)
	stdlog.SetOutput(kitlog.NewStdlibAdapter(logger))

	if *debug {
		logger = level.NewFilter(logger, level.AllowDebug())
	} else {
		logger = level.NewFilter(logger, level.AllowInfo())
	}

	if *storeBackend != "etcdv3" {
		kingpin.Fatalf("unsupported store backend: %s", *storeBackend)
	}

	client, err := clientv3.New(
		clientv3.Config{
			Endpoints:            strings.Split(*storeEndpoints, ","),
			DialTimeout:          *storeDialTimeout,
			DialKeepAliveTime:    *storeKeepaliveTime,
			DialKeepAliveTimeout: *storeKeepaliveTimeout,
		},
	)

	if err != nil {
		kingpin.Fatalf("failed to connect to etcd: %s", err)
	}

	go func() {
		logger.Log("event", "metrics.listen", "address", *metricsAddress, "port", *metricsPort)
		http.Handle("/metrics", promhttp.Handler())
		http.ListenAndServe(fmt.Sprintf("%s:%v", *metricsAddress, *metricsPort), nil)
	}()

	ctx, cancel := setupSignalHandler()
	defer cancel()

	switch command {
	case supervise.FullCommand():
		var g run.Group

		pgBouncer := &pgbouncer.PgBouncer{
			ConfigFile:         *superviseConfigFile,
			ConfigTemplateFile: *superviseConfigTemplateFile,
			Executor: pgbouncer.AuthorizedExecutor{
				User:      *superviseUser,
				Password:  *supervisePassword,
				Database:  *superviseDatabase,
				SocketDir: *superviseSocketDir,
				Port:      *supervisePort,
			},
		}

		{
			var logger = kitlog.With(logger, "component", "pgbouncer.exec")

			if !*superviseExecPgBouncer {
				logger.Log("msg", "not exec'ing PgBouncer- assuming external management")
			} else {
				if err := pgBouncer.GenerateConfig("0.0.0.0"); err != nil {
					kingpin.Fatalf("failed to generate initial PgBouncer config: %v", err)
				}

				cmd := exec.CommandContext(ctx, "pgbouncer", *superviseConfigFile)
				cmd.Stderr = os.Stderr

				g.Add(cmd.Run, func(error) { cancel() })
			}
		}

		{
			var logger = kitlog.With(logger, "component", "pgbouncer.config_management")

			streamOptions := etcd.StreamOptions{
				Ctx:          ctx,
				GetTimeout:   *storeTimeout,
				PollInterval: *supervisePollInterval,
				Keys: []string{
					fmt.Sprintf("%s/%s/clusterdata", *storePrefix, *clusterName),
				},
			}

			retryFoldOptions := streams.RetryFoldOptions{
				Ctx:      ctx,
				Interval: *superviseRetryTimeout,
				Timeout:  *supervisePgBouncerTimeout,
			}

			kvs, _ := etcd.NewStream(logger, client, streamOptions)

			// etcd provides events out-of-order, and potentially duplicated. We need to use the
			// RevisionFilter to ensure we only fold our events in their logical order, without
			// duplicates.
			kvs = streams.RevisionFilter(logger, kvs)

			g.Add(
				func() error {
					return streams.RetryFold(
						logger, kvs, retryFoldOptions,
						func(ctx context.Context, kv *mvccpb.KeyValue) (err error) {
							defer func() {
								if err != nil {
									logger.Log("event", "supervise.error", "error", err, "msg", "failed to respond to change in clusterdata")
								}
							}()

							var clusterdata = &Clusterdata{}
							if err := json.Unmarshal(kv.Value, clusterdata); err != nil {
								return err
							}

							masterAddress := clusterdata.Dbs[clusterdata.Proxy.Spec.MasterDbUID].Status.ListenAddress
							if masterAddress == "" {
								logger.Log("event", "clusterdata.no_master", "msg", "no master found, not reloading PgBouncer")
								return nil
							}

							logger.Log("event", "pgbouncer.reload_configuration", "host", masterAddress)
							if err := pgBouncer.GenerateConfig(masterAddress); err != nil {
								return err
							}

							return pgBouncer.Reload(ctx)
						},
					)
				},
				func(error) { cancel() },
			)
		}

		if err := g.Run(); err != nil {
			logger.Log("error", err.Error(), "msg", "proxy failed, exiting with error")
		}

		logger.Log("event", "shutdown")

	case failover.FullCommand():
		panic("not implemented")
	}
}

// Set by goreleaser
var (
	Version   = "dev"
	Commit    = "none"
	Date      = "unknown"
	GoVersion = runtime.Version()
)

func versionStanza() string {
	return fmt.Sprintf(
		"stolon-pgbouncer Version: %v\nGit SHA: %v\nGo Version: %v\nGo OS/Arch: %v/%v\nBuilt at: %v",
		Version, Commit, GoVersion, runtime.GOOS, runtime.GOARCH, Date,
	)
}

// setupSignalHandler is similar to the community provided functions, but follows a more
// modern pattern using contexts. If the caller desires a channel that will be closed on
// completion, they can simply use ctx.Done()
func setupSignalHandler() (context.Context, func()) {
	ctx, cancel := context.WithCancel(context.Background())

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)

	go func() {
		<-sigc
		cancel()
		<-sigc
		panic("received second signal, exiting immediately")
	}()

	return ctx, cancel
}

package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	stdlog "log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"google.golang.org/grpc"

	"github.com/gocardless/stolon-pgbouncer/pkg/etcd"
	pkgfailover "github.com/gocardless/stolon-pgbouncer/pkg/failover"
	"github.com/gocardless/stolon-pgbouncer/pkg/pgbouncer"
	"github.com/gocardless/stolon-pgbouncer/pkg/stolon"
	"github.com/gocardless/stolon-pgbouncer/pkg/streams"

	"github.com/alecthomas/kingpin"
	tlshelpers "github.com/cloudflare/cfssl/helpers"
	"github.com/coreos/etcd/clientv3"
	"github.com/coreos/etcd/mvcc/mvccpb"
	kitlog "github.com/go-kit/kit/log"
	level "github.com/go-kit/kit/log/level"
	"github.com/grpc-ecosystem/go-grpc-middleware"
	"github.com/oklog/run"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var logger kitlog.Logger

var (
	app = kingpin.New("stolon-pgbouncer", "Tooling to manage PgBouncer with a stolon Postgres cluster").Version(versionStanza())

	// Global flags applying to every command
	debug          = app.Flag("debug", "Enable debug logging").Default("false").Bool()
	metricsAddress = app.Flag("metrics-address", "Address to bind HTTP metrics listener").Default("127.0.0.1").String()
	metricsPort    = app.Flag("metrics-port", "Port to bind HTTP metrics listener").Default("9446").Uint16()

	supervise                           = app.Command("supervise", "Manages local PgBouncer")
	superviseStolonOptions              = newStolonOptions(supervise)
	supervisePgBouncerOptions           = newPgBouncerOptions(supervise)
	supervisePollInterval               = supervise.Flag("poll-interval", "Store poll interval").Default("1m").Duration()
	supervisePgBouncerTimeout           = supervise.Flag("pgbouncer-timeout", "Timeout for PgBouncer operations").Default("5s").Duration()
	supervisePgBouncerRetryTimeout      = supervise.Flag("pgbouncer-retry-timeout", "Retry failed PgBouncer operations at this interval").Default("5s").Duration()
	childProcessTerminationGracePeriod  = supervise.Flag("termination-grace-period", "Pause before rejecting new PgBouncer connections (on shutdown)").Default("5s").Duration()
	childProcessTerminationPollInterval = supervise.Flag("termination-poll-interval", "Poll PgBouncer for outstanding connections at this rate").Default("10s").Duration()

	pauser                 = app.Command("pauser", "Serve the PgBouncer pause API")
	pauserPgBouncerOptions = newPgBouncerOptions(pauser)
	pauserToken            = pauser.Flag("token", "Authentication token for pauser API").Default("").Envar("STBOUNCER_FAILOVER_TOKEN").String()
	pauserBindAddress      = pauser.Flag("bind-address", "Listen address for the pauser API").Default(":8080").String()

	failover                   = app.Command("failover", "Run a zero-downtime failover of the Postgres primary")
	failoverStolonOptions      = newStolonOptions(failover)
	failoverToken              = failover.Flag("token", "Authentication token for pauser API").Default("").Envar("STBOUNCER_FAILOVER_TOKEN").String()
	failoverHealthCheckOnly    = failover.Flag("health-check-only", "Only run the health checks, don't failover").Default("false").Bool()
	failoverPauserPort         = failover.Flag("pauser-port", "Port on which the pauser APIs are listening").Default("8080").String()
	failoverHealthCheckTimeout = failover.Flag("health-check-timeout", "Timeout for health checking pause clients").Default("2s").Duration()
	failoverCleanupTimeout     = failover.Flag("cleanup-timeout", "Timeout for running deferred cleanup operations").Default("10s").Duration()
	failoverLockTimeout        = failover.Flag("lock-timeout", "Timeout for acquiring failover lock").Default("5s").Duration()
	failoverPauseTimeout       = failover.Flag("pause-timeout", "Timeout for pausing PgBouncer").Default("5s").Duration()
	failoverPauseExpiry        = failover.Flag("pause-expiry", "Time to wait before resuming PgBouncer after pause").Default("25s").Duration()
	failoverResumeTimeout      = failover.Flag("resume-timeout", "Timeout for issuing PgBouncer resumes").Default("5s").Duration()
	failoverStolonctlTimeout   = failover.Flag("stolonctl-timeout", "Timeout for executing stolonctl commands").Default("5s").Duration()

	status              = app.Command("status", "Show information about the current status of the cluster")
	statusStolonOptions = newStolonOptions(status)
	statusToken         = status.Flag("token", "Authentication token for pauser API").Default("").Envar("STBOUNCER_FAILOVER_TOKEN").String()
	statusPauserPort    = status.Flag("pauser-port", "Port on which the pauser APIs are listening").Default("8080").String()
	statusTimeout       = status.Flag("timeout", "Timeout for fetching the status").Default("5s").Duration()
)

type stolonOptions struct {
	ClusterName      string
	Backend          string
	Prefix           string
	Endpoints        string
	CAFile           string
	CertFile         string
	KeyFile          string
	SkipTLSVerify    bool
	Timeout          time.Duration
	DialTimeout      time.Duration
	KeepaliveTime    time.Duration
	KeepaliveTimeout time.Duration
}

func newStolonOptions(cmd *kingpin.CmdClause) *stolonOptions {
	opt := &stolonOptions{}

	cmd.Flag("cluster-name", "Name of the stolon cluster").Default("").Envar("STOLONCTL_CLUSTER_NAME").StringVar(&opt.ClusterName)
	cmd.Flag("store-backend", "Store backend provider").Default("etcdv3").Envar("STOLONCTL_STORE_BACKEND").StringVar(&opt.Backend)
	cmd.Flag("store-prefix", "Store prefix").Default("stolon/cluster").Envar("STOLONCTL_STORE_PREFIX").StringVar(&opt.Prefix)
	cmd.Flag("store-endpoints", "Comma delimited list of store endpoints").Envar("STOLONCTL_STORE_ENDPOINTS").Default("http://127.0.0.1:2379").StringVar(&opt.Endpoints)
	cmd.Flag("store-timeout", "Timeout for store operations").Default("3s").DurationVar(&opt.Timeout)
	cmd.Flag("store-dial-timeout", "Timeout when connecting to store").Default("3s").DurationVar(&opt.DialTimeout)
	cmd.Flag("store-keepalive-time", "Time after which client pings server to check transport").Default("30s").DurationVar(&opt.KeepaliveTime)
	cmd.Flag("store-keepalive-timeout", "Timeout for store keepalive probe").Default("5s").DurationVar(&opt.KeepaliveTimeout)
	cmd.Flag("store-ca-file", "Verify certificates of HTTPS-enabled store servers using this CA bundle").Envar("STOLONCTL_STORE_CA_FILE").StringVar(&opt.CAFile)
	cmd.Flag("store-cert-file", "Certificate file for client identification to store").Envar("STOLONCTL_STORE_CERT_FILE").StringVar(&opt.CertFile)
	cmd.Flag("store-key", "Private key file for client identification to the store").Envar("STOLONCTL_STORE_KEY").StringVar(&opt.KeyFile)
	cmd.Flag("store-skip-tls-verify", "Skip store server certificate validation").Envar("STOLONCTL_STORE_SKIP_TLS_VERIFY").BoolVar(&opt.SkipTLSVerify)

	return opt
}

type pgBouncerOptions struct {
	User, Password, Database, SocketDir, Port, ConfigFile, ConfigTemplateFile string
}

func newPgBouncerOptions(cmd *kingpin.CmdClause) *pgBouncerOptions {
	opt := &pgBouncerOptions{}

	cmd.Flag("pgbouncer-user", "Admin user of PgBouncer").Default("pgbouncer").StringVar(&opt.User)
	cmd.Flag("pgbouncer-password", "Password for admin user").Default("").StringVar(&opt.Password)
	cmd.Flag("pgbouncer-database", "PgBouncer special database (inadvisable to change)").Default("pgbouncer").StringVar(&opt.Database)
	cmd.Flag("pgbouncer-socket-dir", "Directory in which the unix socket resides").Default("/var/run/postgresql").StringVar(&opt.SocketDir)
	cmd.Flag("pgbouncer-port", "Directory in which the unix socket resides").Default("6432").StringVar(&opt.Port)
	cmd.Flag("pgbouncer-config-file", "Path to PgBouncer config file").Default("/etc/pgbouncer/pgbouncer.ini").StringVar(&opt.ConfigFile)
	cmd.Flag("pgbouncer-config-template-file", "Path to PgBouncer config template file").Default("/etc/pgbouncer/pgbouncer.ini.template").StringVar(&opt.ConfigTemplateFile)

	return opt
}

var (
	clusterIdentifier = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "stolon_cluster_identifier",
			Help: "Set to 1, is labelled with the cluster_name",
		},
		[]string{"cluster_name"},
	)
	shutdownSeconds = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "stolon_pgbouncer_shutdown_seconds",
			Help: "Shutdown time (received termination signal) since unix epoch in seconds",
		},
	)
	outstandingConnections = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "stolon_pgbouncer_outstanding_connections",
			Help: "Number of outstanding connections in PgBouncer during shutdown",
		},
	)
	storePollInterval = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "stolon_pgbouncer_store_poll_interval",
			Help: "Seconds between each store poll attempt",
		},
	)
	storeLastUpdateSeconds = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "stolon_pgbouncer_store_last_update_seconds",
			Help: "Last time we received a value from our store as seconds since unix epoch",
		},
	)
	lastKeeperSeconds = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "stolon_pgbouncer_last_keeper_seconds",
			Help: "Most recent primary keeper update time since unix epoch in seconds",
		},
		[]string{"keeper"},
	)
	lastReloadSeconds = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "stolon_pgbouncer_last_reload_seconds",
			Help: "Most recent PgBouncer reload time since unix epoch in seconds",
		},
		[]string{"keeper"},
	)
)

func init() {
	prometheus.MustRegister(clusterIdentifier)
	prometheus.MustRegister(shutdownSeconds)
	prometheus.MustRegister(outstandingConnections)
	prometheus.MustRegister(storePollInterval)
	prometheus.MustRegister(storeLastUpdateSeconds)
	prometheus.MustRegister(lastKeeperSeconds)
	prometheus.MustRegister(lastReloadSeconds)
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

	go func() {
		logger.Log("event", "metrics.listen", "address", *metricsAddress, "port", *metricsPort)
		http.Handle("/metrics", promhttp.Handler())
		http.ListenAndServe(fmt.Sprintf("%s:%v", *metricsAddress, *metricsPort), nil)
	}()

	ctx, cancel := setupSignalHandler()
	defer cancel()

	go func() {
		<-ctx.Done()
		shutdownSeconds.SetToCurrentTime()
	}()

	switch command {
	case status.FullCommand():
		stopt := statusStolonOptions

		client := mustStore(stopt)
		clusterdata, _ := mustClusterdata(ctx, client, stopt)
		clients := mustFailoverClients(*clusterdata, *statusPauserPort)

		checks := map[string]pkgfailover.HealthCheckResponse{}
		for keeperUID, client := range clients {
			ctx, cancel := pkgfailover.NewClientCtx(ctx, *statusToken, 10*time.Second)
			defer cancel()

			check, err := client.HealthCheck(ctx, &pkgfailover.Empty{})
			if err != nil {
				logger.Log("event", "healthcheck.failure", "msg", fmt.Sprintf("failed to health check client: %s", err.Error()))
				check = &pkgfailover.HealthCheckResponse{
					Components: []*pkgfailover.HealthCheckResponse_ComponentHealthCheck{
						&pkgfailover.HealthCheckResponse_ComponentHealthCheck{
							Name:   "connect",
							Status: pkgfailover.HealthCheckResponse_UNKNOWN,
							Error:  err.Error(),
						},
					},
				}
			}

			checks[keeperUID] = *check
		}

		renderHealthCheck(checks)

	case failover.FullCommand():
		stopt := failoverStolonOptions

		client := mustStore(stopt)
		clusterdata, key := mustClusterdata(ctx, client, stopt)
		clients := mustFailoverClients(*clusterdata, *failoverPauserPort)

		stolonctl := stolon.Stolonctl{
			ClusterName: stopt.ClusterName, Backend: stopt.Backend, Prefix: stopt.Prefix, Endpoints: stopt.Endpoints,
		}

		// Once our initial context is finished, wait some time before cancelling our defer
		// context. This ensures in the event of an operator SIGQUIT that we attempt to run
		// cleanup tasks before actually quitting.
		deferCtx, cancel := context.WithCancel(context.Background())
		go func() { <-ctx.Done(); time.Sleep(*failoverCleanupTimeout); cancel() }()
		defer cancel()

		opt := pkgfailover.FailoverOptions{
			ClusterdataKey:     key,
			Token:              *failoverToken,
			HealthCheckTimeout: *failoverHealthCheckTimeout,
			LockTimeout:        *failoverLockTimeout,
			PauseTimeout:       *failoverPauseTimeout,
			PauseExpiry:        *failoverPauseExpiry,
			ResumeTimeout:      *failoverResumeTimeout,
			StolonctlTimeout:   *failoverStolonctlTimeout,
		}

		failover := pkgfailover.NewFailover(logger, client, clients, stolonctl, opt)

		var err error
		if *failoverHealthCheckOnly {
			err = failover.HealthCheckClients(ctx)
		} else {
			err = failover.Run(ctx, deferCtx)
		}

		if err != nil {
			logger.Log("error", err, "msg", "exiting with error")
			os.Exit(1)
		}

	case pauser.FullCommand():
		var logger = kitlog.With(logger, "component", "pauser.api")

		listen, err := net.Listen("tcp", *pauserBindAddress)
		if err != nil {
			kingpin.Fatalf("failed to bind to address: %v", err)
		}

		server := pkgfailover.NewServer(logger, mustPgBouncer(pauserPgBouncerOptions))
		grpcServer := grpc.NewServer(
			grpc.UnaryInterceptor(
				grpc_middleware.ChainUnaryServer(
					server.LoggingInterceptor,
					server.NewAuthenticationInterceptor(*pauserToken),
				),
			),
		)
		pkgfailover.RegisterFailoverServer(grpcServer, server)

		go func() {
			<-ctx.Done()
			logger.Log("event", "graceful_shutdown")
			grpcServer.GracefulStop()
		}()

		logger.Log("event", "listen", "address", *pauserBindAddress)
		if err := grpcServer.Serve(listen); err != nil {
			logger.Log("error", err.Error(), "msg", "exiting with error")
			os.Exit(1)
		}

	case supervise.FullCommand():
		var g run.Group

		client := mustStore(superviseStolonOptions)
		pgBouncer := mustPgBouncer(supervisePgBouncerOptions)
		stopt := superviseStolonOptions

		clusterIdentifier.WithLabelValues(stopt.ClusterName).Set(1)
		storePollInterval.Set(float64(*supervisePollInterval / time.Second))

		var logger = kitlog.With(logger, "component", "pgbouncer.child")

		if err := pgBouncer.GenerateConfig("0.0.0.0"); err != nil {
			kingpin.Fatalf("failed to generate initial PgBouncer config: %v", err)
		}

		cmdCtx, cmdCancel := context.WithCancel(context.Background())

		cmd := exec.CommandContext(cmdCtx, "pgbouncer", supervisePgBouncerOptions.ConfigFile)
		cmd.Stderr = os.Stderr

		// Termination handler for PgBouncer. Ensures we only quit PgBouncer once all
		// connections have finished their work.
		g.Add(cmd.Run, func(error) {
			// Whatever happens, once we exit this block we want to terminate the PgBouncer
			// process.
			defer cmdCancel()

			logger.Log("event", "termination_grace_period", "msg", "waiting for grace period")
			time.Sleep(*childProcessTerminationGracePeriod)

			logger.Log("event", "disable", "msg", "disabling new PgBouncer connections")
			{
				ctx, cancel := context.WithTimeout(context.Background(), *supervisePgBouncerTimeout)
				defer cancel()

				if err := pgBouncer.Disable(ctx); err != nil {
					logger.Log("error", err, "msg", "failed to disable PgBouncer")
					return
				}
			}

		PollConnections:

			ctx, cancel := context.WithTimeout(context.Background(), *supervisePgBouncerTimeout)
			defer cancel()

			dbs, err := pgBouncer.ShowDatabases(ctx)
			if err != nil {
				logger.Log("event", "pgbouncer.error", "error", err, "msg", "could not contact PgBouncer")
				goto PollConnections
			}

			var currentConnections = int64(0)
			for _, db := range dbs {
				currentConnections += db.CurrentConnections
				if db.CurrentConnections > 0 {
					logger.Log("event", "outstanding_connections", "database", db.Name, "count", db.CurrentConnections)
				}
			}

			outstandingConnections.Set(float64(currentConnections))

			if currentConnections > 0 {
				logger.Log("event", "shutdown_pending", "total", currentConnections,
					"msg", "waiting for outstanding connections to complete before terminating PgBouncer")
				time.Sleep(*childProcessTerminationPollInterval)
				goto PollConnections
			}

			logger.Log("event", "idle", "msg", "no more connections in PgBouncer, shutting down")
		})

		{
			var logger = kitlog.With(logger, "component", "pgbouncer.watch")

			streamOptions := etcd.StreamOptions{
				Ctx:          ctx,
				GetTimeout:   stopt.Timeout,
				PollInterval: *supervisePollInterval,
				Keys: []string{
					fmt.Sprintf("%s/%s/clusterdata", stopt.Prefix, stopt.ClusterName),
				},
			}

			retryFoldOptions := streams.RetryFoldOptions{
				Ctx:      ctx,
				Interval: *supervisePgBouncerRetryTimeout,
				Timeout:  *supervisePgBouncerTimeout,
			}

			kvs, _ := etcd.NewStream(logger, client, streamOptions)

			// Before we filter revisions, update our last seen metric so we can detect if etcd
			// has become unresponsive.
			kvs = streams.Tap(kvs, func(kv *mvccpb.KeyValue) {
				storeLastUpdateSeconds.SetToCurrentTime()
			})

			// etcd provides events out-of-order, and potentially duplicated. We need to use the
			// RevisionFilter to ensure we only fold our events in their logical order, without
			// duplicates.
			kvs = streams.RevisionFilter(logger, kvs)

			// Track the last reloaded so we can only reload PgBouncer when necessary
			var lastReloadedAddress string

			g.Add(
				func() error {
					return streams.RetryFold(
						logger, kvs, retryFoldOptions,
						func(ctx context.Context, kv *mvccpb.KeyValue) (err error) {
							defer func() {
								if err != nil {
									logger.Log("error", err, "msg", "failed to respond to change in clusterdata")
								}
							}()

							// It's possible for kv to be nil if our stream is being shutdown
							if kv == nil {
								logger.Log("event", "nil_kv", "msg", "nil kv value, channel is shutting down")
								return nil
							}

							var clusterdata = &stolon.Clusterdata{}
							if err := json.Unmarshal(kv.Value, clusterdata); err != nil {
								return err
							}

							master := clusterdata.Master()
							masterAddress := master.Status.ListenAddress
							if masterAddress == "" {
								logger.Log("event", "clusterdata_no_master", "msg", "no master found, not reloading PgBouncer")
								return nil
							}

							// Only try reloading PgBouncer if the host has really changed
							if lastReloadedAddress == masterAddress {
								return nil
							}

							// Set our metric to signal we've received a new keeper. This allows us to
							// compare the time between seeing our new keeper and updating PgBouncer.
							lastKeeperSeconds.Reset()
							lastKeeperSeconds.WithLabelValues(master.Spec.KeeperUID).SetToCurrentTime()

							logger.Log("event", "generate_configuration", "host", master)
							if err := pgBouncer.GenerateConfig(masterAddress); err != nil {
								return err
							}

							logger.Log("event", "reload")
							if err := pgBouncer.Reload(ctx); err != nil {
								return err
							}

							// Mark what we've reloaded to, so we can avoid unnecessary PgBouncer
							// reloads in response to the clusterdata (not the master) changing.
							lastReloadedAddress = masterAddress

							// We only set this metric when we've successfully reloaded PgBouncer with
							// the new keeper value. Alerts should detect when this value is stale when
							// compared to the last known update.
							lastReloadSeconds.Reset()
							lastReloadSeconds.WithLabelValues(master.Spec.KeeperUID).SetToCurrentTime()

							return nil
						},
					)
				},
				func(error) { cancel() },
			)
		}

		if err := g.Run(); err != nil {
			logger.Log("error", err.Error(), "msg", "exiting with error")
		}
	}

	logger.Log("event", "shutdown")
}

// Renders a HealthCheckResponse as human-readable text to stdout
func renderHealthCheck(healthchecks map[string]pkgfailover.HealthCheckResponse) {
	fmt.Printf("\n")
	for client, hc := range healthchecks {
		fmt.Printf("%s: %s", client, pkgfailover.HealthCheckToString(hc))
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

// mustClusterdata leverages the provided stolonOptions and etcd store to fetch
// clusterdata.
func mustClusterdata(ctx context.Context, client *clientv3.Client, stopt *stolonOptions) (*stolon.Clusterdata, string) {
	clusterdataKey := fmt.Sprintf("%s/%s/clusterdata", stopt.Prefix, stopt.ClusterName)
	clusterdata, err := stolon.GetClusterdata(ctx, client, clusterdataKey)
	if err != nil {
		kingpin.Fatalf("failed to get clusterdata: %s", err)
	}

	return clusterdata, clusterdataKey
}

// mustFailoverClient dials all the keepers in the clusterdata returning a map of keeper
// UID to failover clients.
func mustFailoverClients(clusterdata stolon.Clusterdata, port string) map[string]pkgfailover.FailoverClient {
	clients := map[string]pkgfailover.FailoverClient{}
	for _, db := range clusterdata.Dbs {
		logger.Log("event", "client_dial", "client", db)
		conn, err := grpc.Dial(fmt.Sprintf("%s:%s", db.Status.ListenAddress, port), grpc.WithInsecure())
		if err != nil {
			kingpin.Fatalf("failed to dial client %s: %v", db, err)
		}

		clients[db.Spec.KeeperUID] = pkgfailover.NewFailoverClient(conn)
	}

	return clients
}

func mustPgBouncer(opt *pgBouncerOptions) *pgbouncer.PgBouncer {
	return &pgbouncer.PgBouncer{
		ConfigFile:         opt.ConfigFile,
		ConfigTemplateFile: opt.ConfigTemplateFile,
		Executor: pgbouncer.AuthorizedExecutor{
			User:      opt.User,
			Password:  opt.Password,
			Database:  opt.Database,
			SocketDir: opt.SocketDir,
			Port:      opt.Port,
		},
	}
}

func mustStore(opt *stolonOptions) *clientv3.Client {
	if opt.Backend != "etcdv3" {
		kingpin.Fatalf("unsupported store backend: %s", opt.Backend)
	}

	client, err := clientv3.New(
		clientv3.Config{
			TLS:                  mustTLS(opt),
			Endpoints:            strings.Split(opt.Endpoints, ","),
			DialTimeout:          opt.DialTimeout,
			DialKeepAliveTime:    opt.KeepaliveTime,
			DialKeepAliveTimeout: opt.KeepaliveTimeout,
		},
	)

	if err != nil {
		kingpin.Fatalf("failed to connect to etcd: %s", err)
	}

	return client
}

func mustTLS(opt *stolonOptions) *tls.Config {
	if opt.CertFile == "" && opt.KeyFile == "" && opt.CAFile == "" {
		return nil // no TLS required
	}

	var cfg = &tls.Config{}

	if opt.CertFile != "" && opt.KeyFile != "" {
		cert, err := tlshelpers.LoadClientCertificate(opt.CertFile, opt.KeyFile)
		if err != nil {
			kingpin.Fatalf("failed to load client certs: %s", err)
		}

		cfg.Certificates = []tls.Certificate{*cert}
	}

	if opt.CAFile != "" {
		roots, err := tlshelpers.LoadPEMCertPool(opt.CAFile)
		if err != nil {
			kingpin.Fatalf("failed to load CA file: %s", err)
		}

		cfg.RootCAs = roots
	}

	cfg.InsecureSkipVerify = opt.SkipTLSVerify

	return cfg
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

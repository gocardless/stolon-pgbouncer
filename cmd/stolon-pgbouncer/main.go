package main

import (
	"fmt"
	stdlog "log"
	"net/http"
	"os"
	"runtime"

	"github.com/alecthomas/kingpin"
	kitlog "github.com/go-kit/kit/log"
	level "github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var logger kitlog.Logger

var (
	app = kingpin.New("stolon-pgbouncer", "Tooling to manage PgBouncer with a stolon Postgres cluster").Version(versionStanza())

	// Global flags applying to every command
	debug          = app.Flag("debug", "Enable debug logging").Default("false").Bool()
	metricsAddress = app.Flag("metrics-address", "Address to bind HTTP metrics listener").Default("127.0.0.1").String()
	metricsPort    = app.Flag("metrics-port", "Port to bind HTTP metrics listener").Default("9446").Uint16()

	supervise = app.Command("supervise", "Manages local PgBouncer")

	failover = app.Command("failover", "Run a zero-downtime failover of the Postgres primary")
)

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

	switch command {
	case supervise.FullCommand():
		panic("not implemented")
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

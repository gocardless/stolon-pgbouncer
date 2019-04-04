package main

import (
	"context"
	stdlog "log"
	"os"
	"testing"
	"time"

	"github.com/alecthomas/kingpin"
	kitlog "github.com/go-kit/kit/log"
	"github.com/gocardless/stolon-pgbouncer/pkg/acceptance"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var logger kitlog.Logger

var (
	app = kingpin.New("stolon-pgbouncer-acceptance", "Acceptance test suite for stolon-pgbouncer").Version("0.0.0")
)

func main() {
	if _, err := app.Parse(os.Args[1:]); err != nil {
		kingpin.Fatalf("%s, try --help", err)
	}

	if RunSpecs(new(testing.T), "stolon-pgbouncer") {
		os.Exit(0)
	} else {
		os.Exit(1)
	}
}

var _ = Describe("Acceptance", func() {
	logger = kitlog.NewLogfmtLogger(kitlog.NewSyncWriter(os.Stderr))
	logger = kitlog.With(logger, "ts", kitlog.DefaultTimestampUTC, "caller", kitlog.DefaultCaller)

	RegisterFailHandler(Fail)

	SetDefaultEventuallyTimeout(time.Minute)
	SetDefaultEventuallyPollingInterval(100 * time.Millisecond)

	stdlog.SetOutput(kitlog.NewStdlibAdapter(logger))

	acceptance.RunAcceptance(
		context.Background(),
		logger,
	)
})

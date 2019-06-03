//go:generate protoc --go_out=plugins=grpc:. failover.proto

package failover

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc/metadata"

	"github.com/buger/jsonparser"
	"github.com/gocardless/stolon-pgbouncer/pkg/etcd"
	"github.com/gocardless/stolon-pgbouncer/pkg/stolon"
	"github.com/gocardless/stolon-pgbouncer/pkg/streams"

	"github.com/coreos/etcd/clientv3"
	"github.com/coreos/etcd/clientv3/concurrency"
	kitlog "github.com/go-kit/kit/log"
	"github.com/pkg/errors"
)

type Failover struct {
	logger        kitlog.Logger
	client        *clientv3.Client
	clients       map[string]FailoverClient
	stolonctl     stolon.Stolonctl
	sleepInterval string
	locker        locker
	opt           FailoverOptions
}

type FailoverOptions struct {
	ClusterdataKey     string
	Token              string
	HealthCheckTimeout time.Duration
	LockTimeout        time.Duration
	PauseTimeout       time.Duration
	PauseExpiry        time.Duration
	ResumeTimeout      time.Duration
	StolonctlTimeout   time.Duration
}

type locker interface {
	Lock(context.Context) error
	Unlock(context.Context) error
}

// NewClientCtx generates a new context that will authenticate against the pauser API
func NewClientCtx(ctx context.Context, token string, timeout time.Duration) (context.Context, func()) {
	if token != "" {
		md := metadata.Pairs("authorization", token)
		ctx = metadata.NewOutgoingContext(ctx, md)
	}

	return context.WithTimeout(ctx, timeout)
}

func NewFailover(logger kitlog.Logger, client *clientv3.Client, clients map[string]FailoverClient, stolonctl stolon.Stolonctl, opt FailoverOptions) *Failover {
	session, _ := concurrency.NewSession(client)

	return &Failover{
		logger:    logger,
		client:    client,
		clients:   clients,
		stolonctl: stolonctl,
		opt:       opt,
		locker: concurrency.NewMutex(
			session, fmt.Sprintf("%s/failover", opt.ClusterdataKey),
		),
	}
}

// Run triggers the failover process. We model this as a Pipeline of steps, where each
// step has associated deferred actions that must be scheduled before the primary
// operation ever takes place.
//
// This has the benefit of clearly expressing the steps required to perform a failover,
// tidying up some of the error handling and logging noise that would otherwise be
// present.
func (f *Failover) Run(ctx context.Context, deferCtx context.Context) error {
	return Pipeline(
		Step(f.CheckClusterHealthy),
		Step(f.HealthCheckClients),
		Step(f.AcquireLock).Defer(f.ReleaseLock),
		Step(f.ShortenSleepInterval).Defer(f.RestoreSleepInterval),
		Step(f.Pause).Defer(f.Resume),
		Step(f.Failkeeper),
	)(
		ctx, deferCtx,
	)
}

// ShortenSleepInterval temporarily applies a shorter sleep interval that can help stolon
// components respond quicker to the failover. We cache the original interval to ensure we
// can return the cluster to how it was prior to the failover.
func (f *Failover) ShortenSleepInterval(ctx context.Context) error {
	f.logger.Log("event", "cache_original_sleep_interval",
		"msg", "load original sleep interval for replacement after failover")
	cd, err := stolon.GetClusterdataBytes(ctx, f.client, f.opt.ClusterdataKey)
	if err != nil {
		return err
	}

	var interval time.Duration
	f.sleepInterval, err = jsonparser.GetString(cd, "cluster", "spec", "sleepInterval")
	if err == nil {
		interval, err = time.ParseDuration(f.sleepInterval)
	}

	if err != nil {
		return fmt.Errorf("failed to parse sleepInterval: %v", err)
	}

	f.logger.Log("event", "apply_short_sleep_interval", "msg", "apply short sleep interval")
	cd, err = jsonparser.Set(cd, []byte(`"1s"`), "cluster", "spec", "sleepInterval")
	if err != nil {
		return err
	}

	_, err = f.client.Put(ctx, f.opt.ClusterdataKey, string(cd))
	if err != nil {
		return err
	}

	f.logger.Log("event", "wait_until_sleep_interval_applies", "interval", f.sleepInterval,
		"msg", "wait twice the old sleep interval to ensure stolon components have reloaded")
	time.Sleep(2 * interval)

	return nil
}

// RestoreSleepInterval removes the temporary short sleep interval that we apply for the
// purpose of fast failover.
func (f *Failover) RestoreSleepInterval(ctx context.Context) error {
	cd, err := stolon.GetClusterdataBytes(ctx, f.client, f.opt.ClusterdataKey)
	if err != nil {
		return err
	}

	cd, err = jsonparser.Set(cd, []byte(fmt.Sprintf(`"%s"`, f.sleepInterval)), "cluster", "spec", "sleepInterval")
	if err != nil {
		return err
	}

	f.logger.Log("event", "restore_sleep_interval", "interval", f.sleepInterval,
		"msg", "restoring original sleep interval now failover is complete")
	_, err = f.client.Put(ctx, f.opt.ClusterdataKey, string(cd))

	return err
}

func (f *Failover) CheckClusterHealthy(ctx context.Context) error {
	f.logger.Log("event", "check_cluster_healthy", "msg", "checking health of cluster")
	clusterdata, err := stolon.GetClusterdata(ctx, f.client, f.opt.ClusterdataKey)
	if err != nil {
		return err
	}
	return clusterdata.CheckHealthy(1)
}

func (f *Failover) HealthCheckClients(ctx context.Context) error {
	f.logger.Log("event", "clients_health_check", "msg", "health checking all clients")
	for endpoint, client := range f.clients {
		ctx, cancel := NewClientCtx(ctx, f.opt.Token, f.opt.HealthCheckTimeout)
		defer cancel()

		resp, err := client.HealthCheck(ctx, &Empty{})
		if err != nil {
			return errors.Wrapf(err, "client %s failed health check", endpoint)
		}

		if status := resp.GetStatus(); status != HealthCheckResponse_HEALTHY {
			errStr := strings.Builder{}
			for _, c := range resp.GetComponents() {
				fmt.Fprintf(&errStr, "%s: %s\n", c.Name, c.Error)
			}
			return fmt.Errorf("client %s received non-healthy response: %s", endpoint, errStr.String())
		}
	}

	return nil
}

func (f *Failover) AcquireLock(ctx context.Context) error {
	f.logger.Log("event", "etcd_lock_acquire", "msg", "acquiring failover lock in etcd")
	ctx, cancel := context.WithTimeout(ctx, f.opt.LockTimeout)
	defer cancel()

	return f.locker.Lock(ctx)
}

func (f *Failover) ReleaseLock(ctx context.Context) error {
	f.logger.Log("event", "etcd_lock_release", "msg", "releasing failover lock in etcd")
	ctx, cancel := context.WithTimeout(ctx, f.opt.LockTimeout)
	defer cancel()

	return f.locker.Unlock(ctx)
}

func (f *Failover) Pause(ctx context.Context) error {
	logger := kitlog.With(f.logger, "event", "pgbouncer_pause")
	logger.Log("msg", "requesting all pgbouncers pause")

	// Allow an additional second for network round-trip. We should have terminated this
	// request far before this context is expired.
	ctx, cancel := NewClientCtx(ctx, f.opt.Token, f.opt.PauseExpiry+time.Second)
	defer cancel()

	err := f.EachClient(logger, func(endpoint string, client FailoverClient) error {
		_, err := client.Pause(
			ctx, &PauseRequest{
				Timeout: int64(f.opt.PauseTimeout),
				Expiry:  int64(f.opt.PauseExpiry),
			},
		)

		return err
	})

	if err != nil {
		return fmt.Errorf("failed to pause pgbouncers")
	}

	return nil
}

func (f *Failover) Resume(ctx context.Context) error {
	logger := kitlog.With(f.logger, "event", "pgbouncer_resume")
	logger.Log("msg", "requesting all pgbouncers resume")

	ctx, cancel := NewClientCtx(ctx, f.opt.Token, f.opt.ResumeTimeout)
	defer cancel()

	err := f.EachClient(logger, func(endpoint string, client FailoverClient) error {
		_, err := client.Resume(ctx, &Empty{})
		return err
	})

	if err != nil {
		return fmt.Errorf("failed to resume pgbouncers")
	}

	return nil
}

// EachClient provides a helper to perform actions on all the failover clients, in
// parallel. For some operations where there is a penalty for extended running time (such
// as pause) it's important that each request occurs in parallel.
func (f *Failover) EachClient(logger kitlog.Logger, action func(string, FailoverClient) error) (result error) {
	var wg sync.WaitGroup
	for endpoint, client := range f.clients {
		wg.Add(1)

		go func(endpoint string, client FailoverClient) {
			defer func(begin time.Time) {
				logger.Log("endpoint", endpoint, "elapsed", time.Since(begin).Seconds())
				wg.Done()
			}(time.Now())

			if err := action(endpoint, client); err != nil {
				logger.Log("endpoint", endpoint, "error", err.Error())
				result = err
			}
		}(endpoint, client)
	}

	wg.Wait()
	return result
}

// Failkeeper uses stolonctl to mark the current primary keeper as failed
func (f *Failover) Failkeeper(ctx context.Context) error {
	clusterdata, err := stolon.GetClusterdata(ctx, f.client, f.opt.ClusterdataKey)
	if err != nil {
		return err
	}

	master := clusterdata.Master()
	masterKeeperUID := master.Spec.KeeperUID
	if masterKeeperUID == "" {
		return errors.New("could not identify master keeper")
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, f.opt.StolonctlTimeout)
	defer cancel()

	cmd := f.stolonctl.CommandContext(timeoutCtx, "failkeeper", masterKeeperUID)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return errors.Wrap(err, "failed to run stolonctl failkeeper")
	}

	select {
	case <-time.After(f.opt.PauseExpiry):
		return fmt.Errorf("timed out waiting for successful recovery")
	case newMaster := <-f.NotifyRecovered(ctx, f.logger, master):
		f.logger.Log("msg", "cluster successfully recovered", "master", newMaster)
	}

	return nil
}

// NotifyRecovered will return a channel that receives the new master DB only once it is
// healthy and available for writes. We determine this by checking the new master and all
// its sync nodes are healthy.
func (f *Failover) NotifyRecovered(ctx context.Context, logger kitlog.Logger, oldMaster stolon.DB) chan stolon.DB {
	logger = kitlog.With(logger, "key", f.opt.ClusterdataKey)
	logger.Log("msg", "waiting for stolon to report master change")

	kvs, _ := etcd.NewStream(
		f.logger,
		f.client,
		etcd.StreamOptions{
			Ctx:          ctx,
			Keys:         []string{f.opt.ClusterdataKey},
			PollInterval: 5 * time.Second,
			GetTimeout:   time.Second,
		},
	)

	kvs = streams.RevisionFilter(f.logger, kvs)

	notify := make(chan stolon.DB)
	go func() {
		for kv := range kvs {
			if string(kv.Key) != f.opt.ClusterdataKey {
				continue
			}

			var clusterdata = &stolon.Clusterdata{}
			if err := json.Unmarshal(kv.Value, clusterdata); err != nil {
				logger.Log("error", err, "msg", "failed to parse clusterdata update")
				continue
			}

			master := clusterdata.Master()
			if master.Spec.KeeperUID == oldMaster.Spec.KeeperUID {
				logger.Log("event", "pending_failover", "master", master, "msg", "master has not changed nodes")
				continue
			}

			if !master.Status.Healthy {
				logger.Log("event", "master_unhealthy", "master", master, "msg", "new master is unhealthy")
				continue
			}

			healthyStandbys := 0
			for _, standby := range clusterdata.SynchronousStandbys() {
				if standby.Status.Healthy {
					healthyStandbys++
				} else {
					logger.Log("event", "standby_unhealthy", "standby", standby)
				}
			}

			// We can't rely on the keepers updating our new master state to have standbys
			// before the proxy has updated with the new master value. We therefore need to
			// check the cluster specification for the min synchronous standby value so we can
			// detect when the keeper state for the new master hasn't yet been updated, and
			// pause until it has the sufficient number of standbys to accept writes.
			if healthyStandbys < clusterdata.Cluster.Spec.MinSynchronousStandbys {
				logger.Log("event", "insufficient_standbys", "healthy", healthyStandbys,
					"minimum", clusterdata.Cluster.Spec.MinSynchronousStandbys,
					"msg", "do not have enough healthy standbys to satisfy the minSynchronousStandbys")
				continue
			}

			logger.Log("master", master, "msg", "master is available for writes")
			notify <- master

			return
		}

		close(notify)
	}()

	return notify
}

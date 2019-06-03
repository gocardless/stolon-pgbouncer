package stolon

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"reflect"

	"github.com/coreos/etcd/clientv3"
	"github.com/pkg/errors"
)

// GetClusterdata fetches and parses from etcd using the given key
func GetClusterdata(ctx context.Context, client *clientv3.Client, key string) (*Clusterdata, error) {
	clusterdataBytes, err := GetClusterdataBytes(ctx, client, key)
	if err != nil {
		return nil, err
	}

	var clusterdata = &Clusterdata{}
	if err := json.Unmarshal(clusterdataBytes, clusterdata); err != nil {
		return nil, errors.Wrap(err, "failed to parse clusterdata")
	}

	return clusterdata, nil
}

// GetClusterdataBytes returns a byte slice for dynamic manipulation.
func GetClusterdataBytes(ctx context.Context, client *clientv3.Client, key string) ([]byte, error) {
	resp, err := client.Get(ctx, key)
	if err != nil {
		return nil, err
	}

	if len(resp.Kvs) == 0 {
		return nil, errors.New("no clusterdata found")
	}

	return resp.Kvs[0].Value, nil
}

// Clusterdata is a minimal extraction that we need from stolon. Whenever we upgrade
// stolon, we should verify that this definition is compatible.
type Clusterdata struct {
	Cluster `json:"cluster"`
	Proxy   `json:"proxy"`
	Dbs     map[string]DB `json:"dbs"`
}

type Cluster struct {
	Spec ClusterSpec `json:"spec"`
}

type ClusterSpec struct {
	SynchronousReplication bool `json:"synchronousReplication"`
	MinSynchronousStandbys int  `json:"minSynchronousStandbys"`
}

type Proxy struct {
	Spec ProxySpec `json:"spec"`
}

type ProxySpec struct {
	MasterDbUID string `json:"masterDbUid"`
}

type DB struct {
	Spec   DBSpec   `json:"spec"`
	Status DBStatus `json:"status"`
}

type DBSpec struct {
	KeeperUID                   string   `json:"keeperUID"`
	ExternalSynchronousStandbys []string `json:"externalSynchronousStandbys"`
}

type DBStatus struct {
	Healthy             bool     `json:"healthy"`
	ListenAddress       string   `json:"listenAddress"`
	Port                string   `json:"port"`
	SynchronousStandbys []string `json:"synchronousStandbys"`
}

func (d DB) String() string {
	if reflect.DeepEqual(d, &DB{}) {
		return "unknown"
	}

	return fmt.Sprintf("%s (%s)", d.Spec.KeeperUID, d.Status.ListenAddress)
}

func (c Clusterdata) String() string {
	return fmt.Sprintf("master=%s synchronous_standbys=[%v]", c.Master(), c.SynchronousStandbys())
}

func (c Clusterdata) Master() DB {
	return c.Dbs[c.Proxy.Spec.MasterDbUID]
}

// CheckHealthy returns an error if the stolon cluster isn't healthy. Healthy is
// defined as:
//   The master keeper is healthy, and
//   The minimum number of synchronous standby keepers are healthy, and
//   The number of healthy standbys (sync and async) - minimum number of synchronous standbys < the failure tolerance
func (c Clusterdata) CheckHealthy(tolerateFailures int) error {
	if reflect.DeepEqual(c.Master(), &DB{}) {
		return errors.New("no master")
	}
	if !c.Master().Status.Healthy {
		return errors.New("master unhealthy")
	}

	healthyStandbys := 0
	for _, standby := range c.SynchronousStandbys() {
		if standby.Status.Healthy {
			healthyStandbys++
		}
	}

	if healthyStandbys < c.Cluster.Spec.MinSynchronousStandbys {
		return errors.New("insufficient standbys")
	}

	for _, standby := range c.AsynchronousStandbys() {
		if standby.Status.Healthy {
			healthyStandbys++
		}
	}

	if healthyStandbys-c.Cluster.Spec.MinSynchronousStandbys < tolerateFailures {
		return errors.New("insufficient standbys for failure")
	}

	return nil
}

// SynchronousStandbys returns all the DBs that are configured as sync replicas to our
// current primary. If we use a dummy sync replica, then we'll return the empty DB value.
func (c Clusterdata) SynchronousStandbys() []DB {
	dbs := []DB{}
	for _, standbyUID := range c.Master().Status.SynchronousStandbys {
		dbs = append(dbs, c.Dbs[standbyUID])
	}

	for _, standbyUID := range c.Master().Spec.ExternalSynchronousStandbys {
		dbs = append(dbs, c.Dbs[standbyUID])
	}

	return dbs
}

func (c Clusterdata) AsynchronousStandbys() []DB {
	dbs := []DB{}
Loop:
	for _, db := range c.Dbs {
		if db.Spec.KeeperUID == c.Master().Spec.KeeperUID {
			continue Loop
		}
		for _, sync := range c.SynchronousStandbys() {
			if db.Spec.KeeperUID == sync.Spec.KeeperUID {
				continue Loop
			}
		}
		dbs = append(dbs, db)
	}
	return dbs
}

func (c Clusterdata) Databases() []DB {
	dbs := []DB{}
	for _, db := range c.Dbs {
		dbs = append(dbs, db)
	}
	return dbs
}

func (c Clusterdata) ListenAddresses() []string {
	var addrs = []string{}
	for _, db := range c.Dbs {
		addrs = append(addrs, db.Status.ListenAddress)
	}

	return addrs
}

type Stolonctl struct {
	ClusterName, Backend, Prefix, Endpoints string
}

func (s Stolonctl) CommandContext(ctx context.Context, args ...string) *exec.Cmd {
	flags := []string{
		"--cluster-name", s.ClusterName,
		"--store-backend", s.Backend,
		"--store-prefix", s.Prefix,
		"--store-endpoints", s.Endpoints,
	}

	return exec.CommandContext(ctx, "stolonctl", append(args, flags...)...)
}

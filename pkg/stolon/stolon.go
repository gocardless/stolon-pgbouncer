package stolon

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"

	"github.com/coreos/etcd/clientv3"
	"github.com/pkg/errors"
)

// GetClusterdata fetches and parses from etcd using the given key
func GetClusterdata(ctx context.Context, client *clientv3.Client, key string) (*Clusterdata, error) {
	resp, err := client.Get(ctx, key)
	if err != nil {
		return nil, err
	}

	if len(resp.Kvs) == 0 {
		return nil, errors.New("no clusterdata found")
	}

	var clusterdata = &Clusterdata{}
	if err := json.Unmarshal(resp.Kvs[0].Value, clusterdata); err != nil {
		return nil, errors.Wrap(err, "failed to parse clusterdata")
	}

	return clusterdata, nil
}

// Clusterdata is a minimal extraction that we need from stolon. Whenever we upgrade
// stolon, we should verify that this definition is compatible.
type Clusterdata struct {
	Proxy struct {
		Spec struct {
			MasterDbUID string `json:"masterDbUid"`
		} `json:"spec"`
	} `json:"proxy"`

	Dbs map[string]DB `json:"dbs"`
}

type DB struct {
	Spec struct {
		KeeperUID string `json:"keeperUID"`
	} `json:"spec"`
	Status struct {
		Healthy             bool     `json:"healthy"`
		ListenAddress       string   `json:"listenAddress"`
		Port                string   `json:"port"`
		SynchronousStandbys []string `json:"synchronous_standbys"`
	} `json:"status"`
}

func (d DB) String() string {
	return fmt.Sprintf("%s (%s)", d.Spec.KeeperUID, d.Status.ListenAddress)
}

func (c Clusterdata) Master() DB {
	return c.Dbs[c.Proxy.Spec.MasterDbUID]
}

// SynchronousStandbys returns all the DBs that are configured as sync replicas to our
// current primary. If we use a dummy sync replica, then we'll return the empty DB value.
func (c Clusterdata) SynchronousStandbys() []DB {
	dbs := []DB{}
	for _, standbyUID := range c.Master().Status.SynchronousStandbys {
		dbs = append(dbs, c.Dbs[standbyUID])
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
	return exec.CommandContext(
		ctx,
		"stolonctl",
		"--cluster-name", s.ClusterName,
		"--store-backend", s.Backend,
		"--store-prefix", s.Prefix,
		"--store-endpoints", s.Endpoints,
	)
}

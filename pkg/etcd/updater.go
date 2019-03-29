package etcd

import (
	"context"

	"github.com/coreos/etcd/clientv3"
)

// CompareAndUpdate conditionally sets an etcd key value pair if the value differs from
// what is current.
func CompareAndUpdate(ctx context.Context, client *clientv3.Client, key, value string) error {
	txn := client.Txn(ctx).
		If(
			clientv3.Compare(clientv3.Value(key), "=", value),
		).
		Else(
			clientv3.OpPut(key, value),
		)

	_, err := txn.Commit()
	return err
}

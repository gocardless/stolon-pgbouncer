package streams

import (
	"bytes"

	"github.com/coreos/etcd/mvcc/mvccpb"
	kitlog "github.com/go-kit/kit/log"
)

type Filter func(kitlog.Logger, <-chan *mvccpb.KeyValue) <-chan *mvccpb.KeyValue

// DedupeFilter creates a new channel from `in` that emits events provided the value is
// changed from what was previously seen for that key.
func DedupeFilter(logger kitlog.Logger, in <-chan *mvccpb.KeyValue) <-chan *mvccpb.KeyValue {
	out := make(chan *mvccpb.KeyValue)
	lastValues := map[string][]byte{}

	go func() {
		for kv := range in {
			previousValue := lastValues[string(kv.Key)]
			if bytes.Equal(previousValue, kv.Value) {
				withKv(logger, kv).Log("event", "value_unchanged")
			} else {
				out <- kv
				lastValues[string(kv.Key)] = kv.Value
			}
		}

		logger.Log("event", "close", "msg", "in channel closed, closing out")
		close(out)
	}()

	return out
}

// RevisionFilter creates a new channel from `in` that emits every received event,
// provided it preserves ordering of kv ModRevision values on a per-key basis.
func RevisionFilter(logger kitlog.Logger, in <-chan *mvccpb.KeyValue) <-chan *mvccpb.KeyValue {
	out := make(chan *mvccpb.KeyValue)
	lastRevisions := map[string]int64{}

	go func() {
		for kv := range in {
			previous := lastRevisions[string(kv.Key)]
			if previous >= kv.ModRevision {
				withKv(logger, kv).Log("event", "stale_revision", "previous", previous)
			} else {
				out <- kv
				lastRevisions[string(kv.Key)] = kv.ModRevision
			}
		}

		logger.Log("event", "close", "msg", "in channel closed, closing out")
		close(out)
	}()

	return out
}

func withKv(logger kitlog.Logger, kv *mvccpb.KeyValue) kitlog.Logger {
	return kitlog.With(logger, "key", string(kv.Key), "value", string(kv.Value), "revision", kv.ModRevision)
}

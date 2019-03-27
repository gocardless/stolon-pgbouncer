package streams

import (
	"context"
	"time"

	"github.com/coreos/etcd/mvcc/mvccpb"
	kitlog "github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
)

type Operation func(context.Context, *mvccpb.KeyValue) error

// RetryFoldOptions provides configuration to the RetryFold function
type RetryFoldOptions struct {
	Ctx      context.Context
	Interval time.Duration
	Timeout  time.Duration
}

// RetryFold consumes all kvs from the `in` channel and attempts to run an operation on
// them, retrying that operation ad-infinitum in case of errors.
func RetryFold(logger kitlog.Logger, in <-chan *mvccpb.KeyValue, opt RetryFoldOptions, op Operation) error {
	for kv := range in {
		logger := level.Debug(withKv(logger, kv))

	NextAttempt:
		logger.Log("event", "operation.run")
		ctx, cancel := context.WithTimeout(opt.Ctx, opt.Timeout)
		err := op(ctx, kv)
		cancel()

		if err != nil {
			logger.Log("event", "operation.error", "error", err.Error())
			select {
			case newKv := <-in:
				kv = newKv
			case <-time.After(opt.Interval):
				logger.Log("event", "operation.retry")
			}

			goto NextAttempt
		}
	}

	return nil
}

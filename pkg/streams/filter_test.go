package streams_test

import (
	"sync"

	"github.com/coreos/etcd/mvcc/mvccpb"
	kitlog "github.com/go-kit/kit/log"
	"github.com/gocardless/stolon-pgbouncer/pkg/streams"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Stream filters", func() {
	Describe("DedupeFilter", func() {
		var (
			pipe = func(in chan *mvccpb.KeyValue) <-chan *mvccpb.KeyValue {
				return streams.DedupeFilter(kitlog.NewLogfmtLogger(GinkgoWriter), in)
			}
		)

		Context("With duplicate kv", func() {
			var (
				input = []*mvccpb.KeyValue{
					makeKv("/key", "value", 0),
					makeKv("/key", "value", 1),
				}
			)

			It("Sends one message to out channel", func() {
				Expect(collect(input, pipe)).To(
					Equal(
						[]*mvccpb.KeyValue{
							makeKv("/key", "value", 0),
						},
					),
				)
			})
		})

		Context("With different key but same values", func() {
			var (
				input = []*mvccpb.KeyValue{
					makeKv("/key", "value", 0),
					makeKv("/another_key", "value", 0),
				}
			)

			It("Sends both messages", func() {
				Expect(collect(input, pipe)).To(Equal(input))
			})
		})
	})

	Describe("RevisionFilter", func() {
		var (
			pipe = func(in chan *mvccpb.KeyValue) <-chan *mvccpb.KeyValue {
				return streams.RevisionFilter(kitlog.NewLogfmtLogger(GinkgoWriter), in)
			}
		)

		Context("With stale revision", func() {
			var (
				input = []*mvccpb.KeyValue{
					makeKv("/key", "value", 2), // newer
					makeKv("/key", "value", 1), // stale
				}
			)

			It("Sends only the first message", func() {
				Expect(collect(input, pipe)).To(
					Equal(
						[]*mvccpb.KeyValue{
							makeKv("/key", "value", 2),
						},
					),
				)
			})
		})

		Context("With older revision for another key", func() {
			var (
				input = []*mvccpb.KeyValue{
					makeKv("/key", "value", 2),         // newer
					makeKv("/another_key", "value", 1), // older, but different key
				}
			)

			It("Sends both messages", func() {
				Expect(collect(input, pipe)).To(Equal(input))
			})
		})
	})
})

// collect creates an input channel and pipes the contents using the pipe function, then
// pushes all elements in input into the piped channels, returning a slice of all elements
// that are sent through the pipe.
func collect(input []*mvccpb.KeyValue, pipe func(chan *mvccpb.KeyValue) <-chan *mvccpb.KeyValue) []*mvccpb.KeyValue {
	var output = make([]*mvccpb.KeyValue, 0)
	in := make(chan *mvccpb.KeyValue)
	out := pipe(in)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		for _, kv := range input {
			in <- kv
		}

		close(in)
		wg.Done()
	}()

	go func() {
		for kv := range out {
			output = append(output, kv)
		}

		wg.Done()
	}()

	wg.Wait()
	return output
}

func makeKv(key, value string, revision int64) *mvccpb.KeyValue {
	return &mvccpb.KeyValue{Key: []byte(key), Value: []byte(value), ModRevision: revision}
}

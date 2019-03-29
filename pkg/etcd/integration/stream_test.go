package integration

import (
	"context"
	"time"

	"github.com/coreos/etcd/mvcc/mvccpb"
	"github.com/gocardless/stolon-pgbouncer/pkg/etcd"

	kitlog "github.com/go-kit/kit/log"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
)

var _ = Describe("Stream", func() {
	var (
		ctx    context.Context
		cancel func()
		key    string
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
		key = RandomKey()
	})

	AfterEach(func() {
		cancel()
	})

	createStream := func() <-chan *mvccpb.KeyValue {
		stream, _ := etcd.NewStream(
			kitlog.NewLogfmtLogger(GinkgoWriter),
			client,
			etcd.StreamOptions{
				Ctx:          ctx,
				Keys:         []string{key},
				PollInterval: time.Second,
				GetTimeout:   time.Second,
			},
		)

		return stream
	}

	put := func(key, value string) {
		_, err := client.Put(ctx, key, value)
		Expect(err).NotTo(HaveOccurred())
	}

	matchKv := func(key, value string) types.GomegaMatcher {
		return PointTo(
			MatchFields(
				IgnoreExtras,
				Fields{
					"Key":   Equal([]byte(key)),
					"Value": Equal([]byte(value)),
				},
			),
		)
	}

	It("Closes channel when context terminates", func() {
		stream := createStream()
		cancel()

		Eventually(stream).Should(BeClosed())
	})

	Context("When key exists", func() {
		BeforeEach(func() {
			put(key, "initial")
		})

		It("Emits initial value and changes", func() {
			stream := createStream()
			Eventually(stream).Should(Receive(matchKv(key, "initial")))

			put(key, "changed")
			Eventually(stream).Should(Receive(matchKv(key, "changed")))
		})
	})
})

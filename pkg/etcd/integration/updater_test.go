package integration

import (
	"context"
	"time"

	"github.com/coreos/etcd/mvcc/mvccpb"
	"github.com/gocardless/stolon-pgbouncer/pkg/etcd"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
)

var _ = Describe("CompareAndUpdate", func() {
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

	compareAndUpdate := func(value string) error {
		return etcd.CompareAndUpdate(ctx, client, key, value)
	}

	put := func(value string) {
		_, err := client.Put(ctx, key, value)
		Expect(err).NotTo(HaveOccurred())
	}

	get := func() *mvccpb.KeyValue {
		resp, err := client.Get(ctx, key)
		Expect(err).NotTo(HaveOccurred())
		return resp.Kvs[0]
	}

	matchValueRevision := func(value string, modRevision types.GomegaMatcher) types.GomegaMatcher {
		return PointTo(
			MatchFields(IgnoreExtras, Fields{
				"Value":       Equal([]byte(value)),
				"ModRevision": modRevision,
			}),
		)
	}

	Context("When key does not exist", func() {
		It("Creates key", func() {
			Expect(compareAndUpdate("initial")).To(Succeed())
			Expect(get()).To(matchValueRevision("initial", BeNumerically(">", 0)))
		})
	})

	Context("When key exists", func() {
		var (
			initial *mvccpb.KeyValue
		)

		BeforeEach(func() {
			put("initial")
			initial = get()
		})

		Context("With same value", func() {
			It("No-ops update", func() {
				Expect(compareAndUpdate(string(initial.Value))).To(Succeed())
				Expect(get()).To(Equal(initial))
			})
		})

		Context("With different value", func() {
			It("Performs update", func() {
				Expect(compareAndUpdate("changed")).To(Succeed())
				Expect(get()).To(matchValueRevision(
					"changed", BeNumerically(">", initial.ModRevision),
				))
			})
		})
	})
})

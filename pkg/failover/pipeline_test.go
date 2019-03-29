package failover

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

// Dummy error for use when creating steps
var errSample = fmt.Errorf("sample")

var _ = Describe("Pipeline", func() {
	var (
		ctx = context.Background()
		log []string
	)

	BeforeEach(func() {
		log = make([]string, 0)
	})

	stepFunc := func(name string, err error) func(context.Context) error {
		return func(ctx context.Context) error {
			log = append(log, name)
			return err
		}
	}

	Context("When all steps are successful", func() {
		var (
			pipeline = Pipeline(
				Step(stepFunc("a", nil)).Defer(stepFunc("aDefer", nil)),
				Step(stepFunc("b", nil)),
			)
		)

		It("Runs entire pipeline, including deferred", func() {
			err := pipeline(ctx, ctx)

			Expect(err).To(BeNil())
			Expect(log).To(Equal([]string{"a", "b", "aDefer"}))
		})
	})

	Context("When step fails", func() {
		var (
			pipeline = Pipeline(
				Step(stepFunc("a", errSample)).Defer(stepFunc("aDefer", nil)),
				Step(stepFunc("b", nil)),
			)
		)

		It("Runs the step, that steps deferred, but no more", func() {
			err := pipeline(ctx, ctx)

			Expect(err).To(MatchError(errSample))
			Expect(log).To(Equal([]string{"a", "aDefer"}))
		})
	})
})

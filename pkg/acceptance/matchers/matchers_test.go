package matchers

import (
	// "fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
)

// Modifier describes the type of our custom matchers (Any, All)
type Modifier func(types.GomegaMatcher) types.GomegaMatcher

var _ = Describe("matchers", func() {
	DescribeTable("ints",
		func(modifier Modifier, matcher types.GomegaMatcher, input []int, expected bool) {
			match, err := modifier(matcher).Match(input)
			Expect(err).NotTo(HaveOccurred())
			Expect(match).To(Equal(expected))
		},
		Entry("[1,1,1] == All(1)", All, Equal(1), []int{1, 1, 1}, true),
		Entry("[1,1,1] != All(2)", All, Equal(2), []int{1, 1, 1}, false),
		Entry("[1,1,2] != All(1)", All, Equal(1), []int{1, 1, 2}, false),
		Entry("[1,1,1] == Any(1)", Any, Equal(1), []int{1, 1, 1}, true),
		Entry("[1,1,1] != Any(2)", Any, Equal(2), []int{1, 1, 1}, false),
		Entry("[1,1,2] != Any(2)", Any, Equal(2), []int{1, 1, 2}, true),
	)

	DescribeTable("strings",
		func(modifier Modifier, matcher types.GomegaMatcher, input []string, expected bool) {
			match, err := modifier(matcher).Match(input)
			Expect(err).NotTo(HaveOccurred())
			Expect(match).To(Equal(expected))
		},
		Entry("[food,foob,fooooo] == All(/^foo/)", All, HavePrefix("foo"), []string{"food", "foob", "fooooo"}, true),
		Entry("[food,foob,tractors] == All(/^foo/)", All, HavePrefix("foo"), []string{"food", "foob", "tractors"}, false),
		Entry("[food,foob,fooooo] == Any(/^foo/)", Any, HavePrefix("foo"), []string{"food", "foob", "fooooo"}, true),
		Entry("[dyson,loves,tractors] == Any(/^foo/)", Any, HavePrefix("foo"), []string{"dyson", "loves", "tractors"}, false),
	)

})

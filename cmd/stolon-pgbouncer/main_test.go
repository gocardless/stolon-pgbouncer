package main_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Health check endpoint", func() {
	Context("When last master ip update < health check timeout", func() {
		It("It responds 200", func() {

		})
	})

	Context("When last master ip update > health check timeout", func() {
		It("It responds 500", func() {

		})
	})

})

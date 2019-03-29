package integration

import (
	"testing"

	"github.com/coreos/etcd/clientv3"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var (
	client  *clientv3.Client
	cleanup func()
)

// All tests in this suite require access to an etcd cluster. Boot one that we can use for
// everything, and rely on RandomKey() to generate unique keys.
var _ = BeforeSuite(func() {
	client, cleanup = StartEtcd()
})

var _ = AfterSuite(func() {
	cleanup()
})

func TestSuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "pkg/cmd/integration")
}

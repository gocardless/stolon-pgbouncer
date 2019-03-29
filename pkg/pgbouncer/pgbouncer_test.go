package pgbouncer_test

import (
	"io/ioutil"
	"os"

	"github.com/gocardless/stolon-pgbouncer/pkg/pgbouncer"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("PgBouncer", func() {
	var (
		bouncer        *pgbouncer.PgBouncer
		tempConfigFile *os.File
		err            error
	)

	BeforeEach(func() {
		tempConfigFile, err = ioutil.TempFile("", "pgbouncer-config-")
		Expect(err).NotTo(HaveOccurred())

		bouncer = &pgbouncer.PgBouncer{
			ConfigFile:         tempConfigFile.Name(),
			ConfigTemplateFile: "./testdata/pgbouncer.ini.template",
		}
	})

	AfterEach(func() {
		tempConfigFile.Close()
		os.Remove(tempConfigFile.Name())
	})

	Describe("GenerateConfig", func() {
		Context("With valid config template", func() {
			It("Renders new config file", func() {
				Expect(bouncer.GenerateConfig("db.prod")).To(Succeed())
				Expect(ioutil.ReadFile(bouncer.ConfigFile)).To(ContainSubstring("host=db.prod"))
			})
		})

		Context("With missing config template", func() {
			BeforeEach(func() {
				bouncer.ConfigTemplateFile = "/file/does/not/exist"
			})

			It("Returns error", func() {
				Expect(bouncer.GenerateConfig("db.prod")).To(
					MatchError(
						MatchRegexp("failed to read PgBouncer config template file"),
					),
				)
			})
		})
	})
})

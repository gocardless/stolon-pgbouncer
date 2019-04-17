package stolon

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Clusterdata", func() {
	var (
		clusterdata *Clusterdata
	)

	Describe("CheckHealthy", func() {
		var (
			err                    error
			minSynchronousStandbys int
			failures               int
		)

		BeforeEach(func() { failures = 1 })
		BeforeEach(func() { minSynchronousStandbys = 1 })

		Context("Three node cluster", func() {
			var (
				keeper0, keeper1, keeper2 *DB
			)

			// By default, all are healthy
			BeforeEach(func() {
				keeper0 = createKeeper("keeper0", true, []string{"keeper1"})
				keeper1 = createKeeper("keeper1", true, []string{})
				keeper2 = createKeeper("keeper2", true, []string{})
			})

			JustBeforeEach(func() {
				clusterdata = &Clusterdata{
					Cluster: Cluster{
						Spec: ClusterSpec{
							SynchronousReplication: true,
							MinSynchronousStandbys: minSynchronousStandbys,
						},
					},
					Proxy: Proxy{
						Spec: ProxySpec{
							MasterDbUID: "keeper0",
						},
					},
					Dbs: map[string]DB{
						"keeper0": *keeper0,
						"keeper1": *keeper1,
						"keeper2": *keeper2,
					},
				}

				err = clusterdata.CheckHealthy(failures)
			})

			It("Returns no error", func() {
				Expect(err).NotTo(HaveOccurred(), "default cluster is healthy, so expected no error")
			})

			Context("Unhealthy master", func() {
				BeforeEach(func() { keeper0.Status.Healthy = false })

				It("Errors", func() {
					Expect(err).To(MatchError("master unhealthy"))
				})
			})

			Context("Sync is unhealthy", func() {
				BeforeEach(func() { keeper1.Status.Healthy = false })

				It("Errors", func() {
					Expect(err).To(MatchError("insufficient standbys"))
				})
			})

			Context("Async is unhealthy", func() {
				BeforeEach(func() { keeper2.Status.Healthy = false })

				It("Errors", func() {
					Expect(err).To(MatchError("insufficient standbys for failure"))
				})

				// In this situation, we have a failed async but a working sync and master. Given
				// we've asked whether we're healthy in the face of 0 failures, we should return
				// no error, as our cluster is functional even with a single async out.
				Context("With 0 failures tolerated", func() {
					BeforeEach(func() { failures = 0 })

					It("Returns no error", func() {
						Expect(err).NotTo(
							HaveOccurred(), "we are healthy if nothing fails, but we thought we were unhealthy",
						)
					})
				})
			})

			Context("With MinSynchronousStandbys=2", func() {
				BeforeEach(func() { minSynchronousStandbys = 2 })
				BeforeEach(func() { keeper0.Status.SynchronousStandbys = []string{"keeper1", "keeper2"} })

				It("Errors", func() {
					Expect(err).To(
						MatchError("insufficient standbys for failure"),
						"minimum 2 sync standbys required, so we should not be able to survive a %d failures",
						failures,
					)
				})
			})

			Context("With higher desired failures", func() {
				BeforeEach(func() { failures = 2 })

				It("Errors", func() {
					Expect(err).To(
						MatchError("insufficient standbys for failure"),
						"this cluster could not survive %d failures, so we should return an error",
						failures,
					)
				})
			})
		})
	})
})

func createKeeper(uid string, healthy bool, synchronousStandbys []string) *DB {
	return &DB{
		Spec: DBSpec{
			KeeperUID: uid,
		},
		Status: DBStatus{
			Healthy:             healthy,
			SynchronousStandbys: synchronousStandbys,
		},
	}
}

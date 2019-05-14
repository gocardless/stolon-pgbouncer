package main

import (
	"net/http"
	"net/http/httptest"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus"
)

var _ = Describe("Health check endpoint", func() {
	gauge := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "stolon_pbouncer_store_last_update_seconds",
		},
	)

	handler := newHealthCheckHandler(gauge)

	// In production this comes from a CLI flag
	threshold := 60 * time.Second
	superviseStoreUpdateMaxAge = &threshold

	Context("When the last store update happened within the threshold", func() {
		It("It responds 200", func() {
			recorder := httptest.NewRecorder()
			req, err := http.NewRequest("GET", "/health_check", nil)
			Expect(err).ToNot(HaveOccurred())

			gauge.Set(float64(time.Now().Unix()))

			handler(recorder, req)

			Expect(recorder.Result().StatusCode).To(Equal(200))
		})
	})

	Context("When the last store update happened longer ago than the threshold", func() {
		It("It responds 500", func() {
			recorder := httptest.NewRecorder()
			req, err := http.NewRequest("GET", "/health_check", nil)
			Expect(err).ToNot(HaveOccurred())

			gauge.Set(0)

			handler(recorder, req)

			Expect(recorder.Result().StatusCode).To(Equal(500))
		})
	})
})

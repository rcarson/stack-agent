package metrics_test

import (
	"testing"
	"time"

	"github.com/b0rked-dev/steward/internal/metrics"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// gatherCounter gathers all metrics from reg and returns the value of the
// counter with the given name and label pairs (alternating key, value).
func gatherCounter(t *testing.T, reg *prometheus.Registry, name string, labels map[string]string) float64 {
	t.Helper()
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather error: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.GetMetric() {
			if labelsMatch(m.GetLabel(), labels) {
				return m.GetCounter().GetValue()
			}
		}
	}
	return 0
}

// gatherGauge returns the gauge value for the named metric with matching labels.
func gatherGauge(t *testing.T, reg *prometheus.Registry, name string, labels map[string]string) float64 {
	t.Helper()
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather error: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.GetMetric() {
			if labelsMatch(m.GetLabel(), labels) {
				return m.GetGauge().GetValue()
			}
		}
	}
	return 0
}

// gatherHistogramCount returns the sample count for the named histogram with matching labels.
func gatherHistogramCount(t *testing.T, reg *prometheus.Registry, name string, labels map[string]string) uint64 {
	t.Helper()
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather error: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.GetMetric() {
			if labelsMatch(m.GetLabel(), labels) {
				return m.GetHistogram().GetSampleCount()
			}
		}
	}
	return 0
}

func labelsMatch(lbls []*dto.LabelPair, want map[string]string) bool {
	got := make(map[string]string, len(lbls))
	for _, lp := range lbls {
		got[lp.GetName()] = lp.GetValue()
	}
	for k, v := range want {
		if got[k] != v {
			return false
		}
	}
	return true
}

// TestPrometheusRecorder_RecordPoll verifies that RecordPoll increments
// stackagent_polls_total with the correct stack and result labels.
func TestPrometheusRecorder_RecordPoll(t *testing.T) {
	rec := metrics.NewPrometheusRecorder()
	rec.RecordPoll("mystack", "success")
	rec.RecordPoll("mystack", "success")
	rec.RecordPoll("mystack", "error")

	reg := rec.Registry()

	got := gatherCounter(t, reg, "stackagent_polls_total", map[string]string{"stack": "mystack", "result": "success"})
	if got != 2 {
		t.Errorf("expected polls_total success=2, got %v", got)
	}

	got = gatherCounter(t, reg, "stackagent_polls_total", map[string]string{"stack": "mystack", "result": "error"})
	if got != 1 {
		t.Errorf("expected polls_total error=1, got %v", got)
	}
}

// TestPrometheusRecorder_RecordDeploy verifies that RecordDeploy increments
// stackagent_deploys_total, records a histogram observation, and updates the
// last deploy timestamp gauge.
func TestPrometheusRecorder_RecordDeploy(t *testing.T) {
	rec := metrics.NewPrometheusRecorder()
	before := float64(time.Now().Unix())
	rec.RecordDeploy("mystack", "success", 5*time.Second)
	after := float64(time.Now().Unix())

	reg := rec.Registry()

	// Counter incremented.
	got := gatherCounter(t, reg, "stackagent_deploys_total", map[string]string{"stack": "mystack", "result": "success"})
	if got != 1 {
		t.Errorf("expected deploys_total=1, got %v", got)
	}

	// Histogram recorded one observation.
	cnt := gatherHistogramCount(t, reg, "stackagent_deploy_duration_seconds", map[string]string{"stack": "mystack"})
	if cnt != 1 {
		t.Errorf("expected histogram sample_count=1, got %d", cnt)
	}

	// Gauge updated to a reasonable unix timestamp.
	ts := gatherGauge(t, reg, "stackagent_last_deploy_timestamp_seconds", map[string]string{"stack": "mystack"})
	if ts < before || ts > after+1 {
		t.Errorf("expected gauge timestamp between %v and %v, got %v", before, after, ts)
	}
}

// TestNoopRecorder verifies that NoopRecorder methods can be called without panic.
func TestNoopRecorder_NoPanic(t *testing.T) {
	var rec metrics.NoopRecorder
	rec.RecordPoll("s", "ok")
	rec.RecordDeploy("s", "ok", time.Second)
}

package rightsizing

import "testing"

func TestComputeStats_Empty(t *testing.T) {
	s := ComputeStats(nil)
	if s.SampleCount != 0 {
		t.Errorf("SampleCount: got %d, want 0", s.SampleCount)
	}
	if s.Average != 0 || s.P95 != 0 || s.P99 != 0 || s.Max != 0 || s.Latest != 0 {
		t.Errorf("expected all-zero MetricStats for empty input, got %+v", s)
	}
}

func TestComputeStats_SingleValue(t *testing.T) {
	s := ComputeStats([]int64{500})
	if s.SampleCount != 1 {
		t.Errorf("SampleCount: got %d, want 1", s.SampleCount)
	}
	if s.Average != 500 {
		t.Errorf("Average: got %f, want 500", s.Average)
	}
	if s.P95 != 500 {
		t.Errorf("P95: got %f, want 500", s.P95)
	}
	if s.P99 != 500 {
		t.Errorf("P99: got %f, want 500", s.P99)
	}
	if s.Max != 500 {
		t.Errorf("Max: got %f, want 500", s.Max)
	}
	if s.Latest != 500 {
		t.Errorf("Latest: got %f, want 500", s.Latest)
	}
}

func TestComputeStats_MultipleValues(t *testing.T) {
	// sum=5500, avg=550; sorted=[100..1000]
	values := []int64{100, 200, 300, 400, 500, 600, 700, 800, 900, 1000}
	s := ComputeStats(values)

	if s.SampleCount != 10 {
		t.Errorf("SampleCount: got %d, want 10", s.SampleCount)
	}
	// 5500/10 = 550.0 (exact in float64)
	if s.Average != 550.0 {
		t.Errorf("Average: got %f, want 550", s.Average)
	}
	// p95: ceil(0.95*10)-1 = ceil(9.5)-1 = 10-1 = 9 → sorted[9] = 1000
	if s.P95 != 1000.0 {
		t.Errorf("P95: got %f, want 1000", s.P95)
	}
	// p99: ceil(0.99*10)-1 = ceil(9.9)-1 = 10-1 = 9 → sorted[9] = 1000
	if s.P99 != 1000.0 {
		t.Errorf("P99: got %f, want 1000", s.P99)
	}
	if s.Max != 1000.0 {
		t.Errorf("Max: got %f, want 1000", s.Max)
	}
	// Latest = last element of the original slice (time-ordered, not sorted) = 1000
	if s.Latest != 1000.0 {
		t.Errorf("Latest: got %f, want 1000", s.Latest)
	}
}

func TestComputeStats_LatestDistinctFromMax(t *testing.T) {
	// Descending input: latest sample (100) differs from max (1000).
	// A bug reading sorted[n-1] instead of values[n-1] would fail this test.
	values := []int64{1000, 900, 800, 700, 600, 500, 400, 300, 200, 100}
	s := ComputeStats(values)
	if s.Latest != 100.0 {
		t.Errorf("Latest: got %f, want 100 (last time-ordered sample)", s.Latest)
	}
	if s.Max != 1000.0 {
		t.Errorf("Max: got %f, want 1000", s.Max)
	}
}

func TestComputeStats_DoesNotMutateInput(t *testing.T) {
	input := []int64{3, 1, 2}
	ComputeStats(input)
	if input[0] != 3 || input[1] != 1 || input[2] != 2 {
		t.Errorf("ComputeStats modified the input slice: got %v", input)
	}
}

func TestPercentile_KnownValues(t *testing.T) {
	sorted := []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	tests := []struct {
		p    float64
		want float64
	}{
		{0.00, 1},  // ceil(0*10)-1 = -1 → max(-1,0)=0 → sorted[0] = 1
		{0.50, 5},  // ceil(0.5*10)-1 = 4 → sorted[4] = 5
		{0.95, 10}, // ceil(9.5)-1 = 9 → sorted[9] = 10
		{0.99, 10}, // ceil(9.9)-1 = 9 → sorted[9] = 10
		{1.00, 10}, // ceil(10.0)-1 = 9 → sorted[9] = 10
	}
	for _, tt := range tests {
		got := Percentile(sorted, tt.p)
		if got != tt.want {
			t.Errorf("Percentile(sorted, %v) = %v, want %v", tt.p, got, tt.want)
		}
	}
}

func TestPercentile_Empty(t *testing.T) {
	got := Percentile(nil, 0.95)
	if got != 0 {
		t.Errorf("Percentile(nil, 0.95) = %v, want 0", got)
	}
}

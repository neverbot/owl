package chunkenc

import (
	"math"
	"math/rand/v2"
	"testing"
)

// drain pulls every sample from an iterator into a slice for
// comparison. Asserts no decode error.
func drain(t *testing.T, it *Iter) []Sample {
	t.Helper()
	out := []Sample{}
	for {
		s, ok := it.Next()
		if !ok {
			break
		}
		out = append(out, s)
	}
	if err := it.Err(); err != nil {
		t.Fatalf("iter err: %v", err)
	}
	return out
}

func TestChunkEmpty(t *testing.T) {
	c := NewChunk()
	it, err := Iterator(c.Bytes())
	if err != nil {
		t.Fatalf("Iterator: %v", err)
	}
	if got := drain(t, it); len(got) != 0 {
		t.Errorf("empty chunk yielded %v", got)
	}
}

func TestChunkSingleSample(t *testing.T) {
	c := NewChunk()
	c.Append(1700000000000, 3.14)
	it, _ := Iterator(c.Bytes())
	got := drain(t, it)
	if len(got) != 1 || got[0].TS != 1700000000000 || got[0].Value != 3.14 {
		t.Errorf("got %+v", got)
	}
}

func TestChunkTwoSamples(t *testing.T) {
	c := NewChunk()
	c.Append(100, 1.0)
	c.Append(200, 2.0)
	it, _ := Iterator(c.Bytes())
	got := drain(t, it)
	want := []Sample{{TS: 100, Value: 1.0}, {TS: 200, Value: 2.0}}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("[%d]: got %+v want %+v", i, got[i], want[i])
		}
	}
}

func TestChunkRegularCadenceConstantValueIsTiny(t *testing.T) {
	c := NewChunk()
	const interval = 15000
	for i := 0; i < 100; i++ {
		c.Append(int64(i)*interval, 42.0)
	}
	b := c.Bytes()
	t.Logf("100 constant samples encoded as %d bytes", len(b))
	if len(b) > 60 {
		t.Errorf("expected <60 bytes for 100 constant samples on regular cadence, got %d", len(b))
	}
	// Round-trip.
	it, _ := Iterator(b)
	got := drain(t, it)
	if len(got) != 100 {
		t.Fatalf("got %d samples, want 100", len(got))
	}
	for i, s := range got {
		if s.TS != int64(i)*interval || s.Value != 42.0 {
			t.Errorf("[%d]: %+v", i, s)
		}
	}
}

func TestChunkRegularCadenceCounter(t *testing.T) {
	c := NewChunk()
	const interval = 5000
	for i := 0; i < 1000; i++ {
		c.Append(int64(i)*interval, float64(i))
	}
	b := c.Bytes()
	t.Logf("1000 counter samples encoded as %d bytes (%.2f bits/sample)",
		len(b), float64(len(b))*8/1000)
	it, _ := Iterator(b)
	got := drain(t, it)
	for i, s := range got {
		if s.TS != int64(i)*interval {
			t.Errorf("[%d] ts = %d, want %d", i, s.TS, int64(i)*interval)
		}
		if s.Value != float64(i) {
			t.Errorf("[%d] val = %v, want %d", i, s.Value, i)
		}
	}
}

// Property test: random (monotonic ts, arbitrary float64) sequences
// round-trip losslessly.
func TestChunkPropertyRandomSequences(t *testing.T) {
	for seed := uint64(1); seed <= 50; seed++ {
		r := rand.New(rand.NewPCG(seed, seed*7919))
		n := 1 + r.IntN(500)
		c := NewChunk()
		samples := make([]Sample, 0, n)
		ts := int64(1700000000000) + r.Int64N(1_000_000)
		for i := 0; i < n; i++ {
			// Random monotonic gap, 1ms..30s.
			ts += 1 + r.Int64N(30_000)
			v := r.Float64() * float64(r.IntN(1_000_000))
			if r.IntN(20) == 0 {
				v = 0
			}
			if r.IntN(30) == 0 {
				v = math.Inf(1)
			}
			samples = append(samples, Sample{TS: ts, Value: v})
			c.Append(ts, v)
		}
		it, err := Iterator(c.Bytes())
		if err != nil {
			t.Fatalf("seed %d: Iterator err: %v", seed, err)
		}
		got := drain(t, it)
		if len(got) != len(samples) {
			t.Fatalf("seed %d: len got=%d want=%d", seed, len(got), len(samples))
		}
		for i := range got {
			if got[i].TS != samples[i].TS {
				t.Errorf("seed %d [%d] ts: got %d want %d", seed, i, got[i].TS, samples[i].TS)
			}
			// NaN equality is special; everything else must be exact-bit.
			gb := math.Float64bits(got[i].Value)
			wb := math.Float64bits(samples[i].Value)
			if gb != wb {
				t.Errorf("seed %d [%d] val: got %x want %x", seed, i, gb, wb)
			}
		}
	}
}

func TestChunkPropertyNaNRoundTrips(t *testing.T) {
	c := NewChunk()
	c.Append(0, math.NaN())
	c.Append(1, 1.0)
	c.Append(2, math.NaN())
	c.Append(3, math.NaN())
	c.Append(4, 0)

	it, _ := Iterator(c.Bytes())
	got := drain(t, it)
	wantNaN := []bool{true, false, true, true, false}
	for i, w := range wantNaN {
		if w != math.IsNaN(got[i].Value) {
			t.Errorf("[%d]: IsNaN=%v want %v (val=%v)", i, math.IsNaN(got[i].Value), w, got[i].Value)
		}
	}
}

func TestChunkPropertyIrregularCadence(t *testing.T) {
	c := NewChunk()
	tss := []int64{0, 5000, 5050, 10000, 100000, 100001, 100002, 110000}
	vals := []float64{1.5, 1.6, 1.6, 1.55, 1.5, 1.5, 1.5, 1.49}
	for i, ts := range tss {
		c.Append(ts, vals[i])
	}
	it, _ := Iterator(c.Bytes())
	got := drain(t, it)
	for i := range got {
		if got[i].TS != tss[i] || got[i].Value != vals[i] {
			t.Errorf("[%d]: got %+v want {%d, %v}", i, got[i], tss[i], vals[i])
		}
	}
}

func TestChunkAccessors(t *testing.T) {
	c := NewChunk()
	if c.Count() != 0 {
		t.Errorf("Count empty = %d", c.Count())
	}
	c.Append(10, 1)
	c.Append(20, 2)
	c.Append(35, 3)
	if c.Count() != 3 {
		t.Errorf("Count = %d, want 3", c.Count())
	}
	if c.StartTS() != 10 {
		t.Errorf("StartTS = %d, want 10", c.StartTS())
	}
	if c.EndTS() != 35 {
		t.Errorf("EndTS = %d, want 35", c.EndTS())
	}
}

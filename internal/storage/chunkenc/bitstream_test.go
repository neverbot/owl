package chunkenc

import (
	"math/rand/v2"
	"testing"
)

func TestBitstreamRoundTripSingleBits(t *testing.T) {
	w := bstream{}
	bits := []bool{true, false, true, true, false, false, true, false, true, true, true}
	for _, b := range bits {
		w.writeBit(b)
	}
	r := newBReader(w.bytes())
	for i, want := range bits {
		got, ok := r.readBit()
		if !ok {
			t.Fatalf("readBit(%d) EOF", i)
		}
		if got != want {
			t.Errorf("readBit(%d) = %v, want %v", i, got, want)
		}
	}
}

func TestBitstreamRoundTripBitGroups(t *testing.T) {
	w := bstream{}
	type wr struct {
		v     uint64
		nbits int
	}
	writes := []wr{
		{0xAB, 8},
		{0x3, 2},
		{0xFFFFFFFF, 32},
		{0x1, 1},
		{0xCAFEBABE, 32},
		{0x7F, 7},
		{0x123456789ABCDEF0, 64},
	}
	for _, w0 := range writes {
		w.writeBits(w0.v, w0.nbits)
	}
	r := newBReader(w.bytes())
	for i, w0 := range writes {
		got, ok := r.readBits(w0.nbits)
		if !ok {
			t.Fatalf("readBits(%d) EOF", i)
		}
		mask := uint64(0)
		if w0.nbits == 64 {
			mask = ^uint64(0)
		} else {
			mask = (uint64(1) << w0.nbits) - 1
		}
		if got != (w0.v & mask) {
			t.Errorf("readBits(%d, nbits=%d) = %x, want %x", i, w0.nbits, got, w0.v&mask)
		}
	}
}

func TestBitstreamMixedSingleAndGroup(t *testing.T) {
	w := bstream{}
	w.writeBit(true)
	w.writeBits(0xFF, 8)
	w.writeBit(false)
	w.writeBits(0x5A5A, 16)
	w.writeBit(true)

	r := newBReader(w.bytes())
	if v, _ := r.readBit(); !v {
		t.Error("expected true")
	}
	if v, _ := r.readBits(8); v != 0xFF {
		t.Errorf("got %x, want 0xFF", v)
	}
	if v, _ := r.readBit(); v {
		t.Error("expected false")
	}
	if v, _ := r.readBits(16); v != 0x5A5A {
		t.Errorf("got %x, want 0x5A5A", v)
	}
	if v, _ := r.readBit(); !v {
		t.Error("expected true")
	}
}

// Property test: random sequence of writes round-trips losslessly.
func TestBitstreamPropertyRandomSequences(t *testing.T) {
	for seed := uint64(1); seed <= 50; seed++ {
		r := rand.New(rand.NewPCG(seed, seed*31))
		var w bstream
		type op struct {
			v     uint64
			nbits int
		}
		ops := make([]op, 0, 200)
		for i := 0; i < 200; i++ {
			nbits := r.IntN(64) + 1
			var v uint64
			if nbits == 64 {
				v = r.Uint64()
			} else {
				v = r.Uint64() & ((uint64(1) << nbits) - 1)
			}
			ops = append(ops, op{v, nbits})
			w.writeBits(v, nbits)
		}
		reader := newBReader(w.bytes())
		for i, o := range ops {
			got, ok := reader.readBits(o.nbits)
			if !ok {
				t.Fatalf("seed %d op %d EOF", seed, i)
			}
			if got != o.v {
				t.Fatalf("seed %d op %d nbits=%d: got %x, want %x", seed, i, o.nbits, got, o.v)
			}
		}
	}
}

package chunkenc

import (
	"encoding/binary"
	"errors"
	"math"
)

// Chunk is a write-only builder for a Gorilla-encoded sample run. A
// Chunk lives for the lifetime of one flush: the writer appends
// samples in monotonic timestamp order, then calls Bytes() to obtain
// the serialised blob.
//
// Wire format (big-endian throughout):
//
//	u16 count                     number of samples
//	i64 firstTS                   first timestamp (millis since epoch)
//	f64 firstValue                first value, raw bits
//	i32 firstDelta                ts[1] - ts[0] in milliseconds; zero when count == 1
//	f64 secondValue               second value, raw bits; absent when count <= 1
//	bitstream                     the remaining (count-2) samples, encoded with delta-of-delta + Gorilla XOR
//
// The header is fixed-size (22 bytes for count<=1, 34 for count==2,
// 34 + bitstream for count>=3) so the iterator can find the bitstream
// without a separate offset.
type Chunk struct {
	count    uint16
	firstTS  int64
	firstVal float64
	// firstDelta is the gap between sample #1 and #2 in milliseconds.
	// Captured once at the second Append and never overwritten; the
	// running lastDelta below tracks subsequent gaps.
	firstDelta int64
	// Bookkeeping for the delta-of-delta encoder (timestamps).
	lastTS    int64
	lastDelta int64
	// Bookkeeping for the XOR encoder (values).
	lastVal      float64
	lastLeading  uint8
	lastTrailing uint8
	// secondVal is captured raw at append #2 so that the iterator can
	// bootstrap the delta-of-delta and XOR state machines without
	// running their forward steps backwards.
	secondVal float64
	// bs accumulates the bitstream for sample index >= 2 only.
	bs bstream
}

// NewChunk returns an empty Chunk ready for samples.
func NewChunk() *Chunk { return &Chunk{} }

// Append adds one (ts, value) pair to the chunk. Timestamps must be
// monotonically non-decreasing, in milliseconds. Appending after
// Bytes() has been called is undefined.
func (c *Chunk) Append(ts int64, v float64) {
	switch c.count {
	case 0:
		c.firstTS = ts
		c.firstVal = v
		c.lastTS = ts
		c.lastVal = v
		c.count = 1
		return
	case 1:
		// Sample #2: capture the initial delta and the raw value. No
		// bitstream writes yet — the iterator decodes #2 from the
		// fixed-size header. This avoids needing an "initial leading"
		// sentinel in the XOR state.
		c.firstDelta = ts - c.lastTS
		c.lastDelta = c.firstDelta
		c.lastTS = ts
		c.secondVal = v
		c.lastVal = v
		// Reset XOR state for the new "previous value" baseline.
		c.lastLeading = 0xff
		c.lastTrailing = 0
		c.count = 2
		return
	}
	// Sample #3 onwards: encode delta-of-delta for ts, XOR for value.
	c.writeDeltaOfDelta(ts)
	c.writeXOR(v)
	c.lastTS = ts
	c.lastVal = v
	c.count++
}

// writeDeltaOfDelta encodes a timestamp using the same buckets the
// Gorilla paper proposes, matching Prometheus' tsdb implementation:
//
//	dod = 0                          → 1 bit:    0
//	-63       <= dod <= 64           → 2 bits:   10  + 7 bits signed
//	-255      <= dod <= 256          → 3 bits:   110 + 9 bits signed
//	-2047     <= dod <= 2048         → 4 bits:   1110 + 12 bits signed
//	otherwise                        → 4 bits:   1111 + 32 bits signed
//
// In owl, scrape cadence is regular (every 5/10/15 s), so dod is zero
// almost always and most timestamps cost a single bit.
func (c *Chunk) writeDeltaOfDelta(ts int64) {
	delta := ts - c.lastTS
	dod := delta - c.lastDelta
	switch {
	case dod == 0:
		c.bs.writeBit(false)
	case bitRange(dod, 7):
		c.bs.writeBits(0b10, 2)
		c.bs.writeBits(uint64(dod)&((1<<7)-1), 7)
	case bitRange(dod, 9):
		c.bs.writeBits(0b110, 3)
		c.bs.writeBits(uint64(dod)&((1<<9)-1), 9)
	case bitRange(dod, 12):
		c.bs.writeBits(0b1110, 4)
		c.bs.writeBits(uint64(dod)&((1<<12)-1), 12)
	default:
		c.bs.writeBits(0b1111, 4)
		c.bs.writeBits(uint64(int32(dod))&((1<<32)-1), 32)
	}
	c.lastDelta = delta
}

// writeXOR encodes a value using Gorilla XOR float compression:
//
//	xor == 0                                       → 1 bit:  0
//	xor != 0 and meaningful bits inside previous   → 2 bits: 10 + meaningful bits
//	xor != 0 and meaningful bits outside           → 2 bits: 11 + 5 bits leading + 6 bits length + meaningful bits
//
// For owl, gauges that change slowly produce xor=0 (or near-zero with
// small meaningful blocks) for the bulk of samples.
func (c *Chunk) writeXOR(v float64) {
	xor := math.Float64bits(v) ^ math.Float64bits(c.lastVal)
	if xor == 0 {
		c.bs.writeBit(false)
		return
	}
	c.bs.writeBit(true)
	leading := uint8(leadingZeros64(xor))
	trailing := uint8(trailingZeros64(xor))
	// Clamp leading to 31 (the field is 5 bits wide when emitted).
	if leading >= 32 {
		leading = 31
	}
	if c.lastLeading != 0xff && leading >= c.lastLeading && trailing >= c.lastTrailing {
		// Reuse the previous (leading, trailing) window.
		c.bs.writeBit(false)
		meaningful := 64 - int(c.lastLeading) - int(c.lastTrailing)
		c.bs.writeBits(xor>>c.lastTrailing, meaningful)
	} else {
		// Emit a fresh window.
		c.bs.writeBit(true)
		c.bs.writeBits(uint64(leading), 5)
		meaningful := 64 - int(leading) - int(trailing)
		// Gorilla writes 6 bits of "meaningful bit count". A value of
		// 0 encodes 64 meaningful bits in the paper; we follow that
		// convention but clamp to 63 because owl will never legally
		// see a 64-bit-meaningful XOR with leading=trailing=0 (would
		// imply value flipped every bit).
		if meaningful == 64 {
			meaningful = 63
		}
		c.bs.writeBits(uint64(meaningful), 6)
		c.bs.writeBits(xor>>trailing, meaningful)
		c.lastLeading = leading
		c.lastTrailing = trailing
	}
}

// bitRange returns true when v fits in `bits` two's-complement bits.
func bitRange(v int64, bits uint) bool {
	max := int64(1) << (bits - 1)
	return v >= -max && v < max
}

// Bytes serialises the chunk to its on-disk form.
func (c *Chunk) Bytes() []byte {
	out := make([]byte, 0, 22+len(c.bs.bytes()))
	out = binary.BigEndian.AppendUint16(out, c.count)
	if c.count == 0 {
		return out
	}
	out = binary.BigEndian.AppendUint64(out, uint64(c.firstTS))
	out = binary.BigEndian.AppendUint64(out, math.Float64bits(c.firstVal))
	if c.count == 1 {
		return out
	}
	// firstDelta as int32 (milliseconds). For owl-scale scrape cadences
	// (≤24 days between samples) int32 is plenty.
	out = binary.BigEndian.AppendUint32(out, uint32(int32(c.firstDelta)))
	out = binary.BigEndian.AppendUint64(out, math.Float64bits(c.secondVal))
	out = append(out, c.bs.bytes()...)
	return out
}

// Count returns the number of samples appended so far.
func (c *Chunk) Count() int { return int(c.count) }

// StartTS returns the timestamp of the first appended sample.
// Panics if Count() == 0.
func (c *Chunk) StartTS() int64 { return c.firstTS }

// EndTS returns the timestamp of the most recently appended sample.
// Panics if Count() == 0.
func (c *Chunk) EndTS() int64 { return c.lastTS }

// Sample is one (ts, value) point yielded by an Iterator.
type Sample struct {
	TS    int64
	Value float64
}

// Iter walks the samples in a serialised chunk.
type Iter struct {
	count uint16
	idx   uint16

	// Header-derived state.
	firstTS    int64
	firstVal   float64
	firstDelta int64
	secondVal  float64

	// Decoder state for the bitstream tail.
	r            bstreamReader
	lastTS       int64
	lastDelta    int64
	lastVal      float64
	lastLeading  uint8
	lastTrailing uint8

	err error
}

// Iterator parses the chunk header and returns a forward-only Iter.
// The data slice is referenced (not copied); callers must keep it
// alive for the lifetime of the iterator.
func Iterator(data []byte) (*Iter, error) {
	if len(data) < 2 {
		return nil, errors.New("chunkenc: truncated chunk (count)")
	}
	it := &Iter{count: binary.BigEndian.Uint16(data)}
	if it.count == 0 {
		return it, nil
	}
	if len(data) < 18 {
		return nil, errors.New("chunkenc: truncated chunk (first sample)")
	}
	it.firstTS = int64(binary.BigEndian.Uint64(data[2:]))
	it.firstVal = math.Float64frombits(binary.BigEndian.Uint64(data[10:]))
	if it.count == 1 {
		return it, nil
	}
	if len(data) < 30 {
		return nil, errors.New("chunkenc: truncated chunk (second sample)")
	}
	it.firstDelta = int64(int32(binary.BigEndian.Uint32(data[18:])))
	it.secondVal = math.Float64frombits(binary.BigEndian.Uint64(data[22:]))
	it.r = newBReader(data[30:])
	return it, nil
}

// Next yields the next sample. ok is false when the chunk is exhausted
// or when decoding fails (check Err()).
func (it *Iter) Next() (Sample, bool) {
	if it.idx >= it.count {
		return Sample{}, false
	}
	switch it.idx {
	case 0:
		it.idx++
		it.lastTS = it.firstTS
		it.lastVal = it.firstVal
		return Sample{TS: it.firstTS, Value: it.firstVal}, true
	case 1:
		it.idx++
		it.lastDelta = it.firstDelta
		ts := it.firstTS + it.firstDelta
		it.lastTS = ts
		it.lastVal = it.secondVal
		// Reset XOR state for the bitstream that follows.
		it.lastLeading = 0xff
		it.lastTrailing = 0
		return Sample{TS: ts, Value: it.secondVal}, true
	}
	dod, ok := it.readDoD()
	if !ok {
		it.err = errors.New("chunkenc: truncated dod")
		return Sample{}, false
	}
	delta := it.lastDelta + dod
	ts := it.lastTS + delta
	v, ok := it.readXOR()
	if !ok {
		it.err = errors.New("chunkenc: truncated xor")
		return Sample{}, false
	}
	it.lastDelta = delta
	it.lastTS = ts
	it.lastVal = v
	it.idx++
	return Sample{TS: ts, Value: v}, true
}

// Err returns the decoding error, if any.
func (it *Iter) Err() error { return it.err }

func (it *Iter) readDoD() (int64, bool) {
	// Read prefix: count consecutive 1 bits up to four.
	var prefix int
	for prefix < 4 {
		bit, ok := it.r.readBit()
		if !ok {
			return 0, false
		}
		if !bit {
			break
		}
		prefix++
	}
	var nbits int
	switch prefix {
	case 0:
		return 0, true
	case 1:
		nbits = 7
	case 2:
		nbits = 9
	case 3:
		nbits = 12
	case 4:
		nbits = 32
	}
	u, ok := it.r.readBits(nbits)
	if !ok {
		return 0, false
	}
	return signExtend(u, nbits), true
}

func (it *Iter) readXOR() (float64, bool) {
	bit, ok := it.r.readBit()
	if !ok {
		return 0, false
	}
	if !bit {
		// xor == 0
		return it.lastVal, true
	}
	bit, ok = it.r.readBit()
	if !ok {
		return 0, false
	}
	var leading, trailing uint8
	if bit {
		// Fresh window.
		l, ok := it.r.readBits(5)
		if !ok {
			return 0, false
		}
		leading = uint8(l)
		m, ok := it.r.readBits(6)
		if !ok {
			return 0, false
		}
		meaningful := int(m)
		if meaningful == 0 {
			meaningful = 64
		}
		trailing = uint8(64 - int(leading) - meaningful)
		xor, ok := it.r.readBits(meaningful)
		if !ok {
			return 0, false
		}
		v := math.Float64frombits(math.Float64bits(it.lastVal) ^ (xor << trailing))
		it.lastLeading = leading
		it.lastTrailing = trailing
		return v, true
	}
	// Reuse previous window.
	if it.lastLeading == 0xff {
		// Decoder is out of sync: a "reuse" bit cannot precede the
		// first fresh window. Treat as corruption.
		return 0, false
	}
	meaningful := 64 - int(it.lastLeading) - int(it.lastTrailing)
	xor, ok := it.r.readBits(meaningful)
	if !ok {
		return 0, false
	}
	v := math.Float64frombits(math.Float64bits(it.lastVal) ^ (xor << it.lastTrailing))
	return v, true
}

// signExtend treats u as an `nbits`-wide two's-complement value and
// extends it to int64.
func signExtend(u uint64, nbits int) int64 {
	shift := 64 - nbits
	return int64(u<<shift) >> shift
}

// leadingZeros64 / trailingZeros64 — small helpers so the package
// keeps zero non-stdlib imports. The math/bits stdlib would also work
// and the compiler intrinsics it (kept here only because the file is
// small and the helpers self-document the algorithm).
func leadingZeros64(x uint64) int {
	if x == 0 {
		return 64
	}
	n := 0
	for x&(1<<63) == 0 {
		x <<= 1
		n++
	}
	return n
}

func trailingZeros64(x uint64) int {
	if x == 0 {
		return 64
	}
	n := 0
	for x&1 == 0 {
		x >>= 1
		n++
	}
	return n
}

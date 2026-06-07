// Package chunkenc implements Gorilla-style compression for owl's
// time-series chunks. The format combines XOR float compression (for
// values) with delta-of-delta integer compression (for timestamps),
// both described in the Facebook Gorilla paper (VLDB 2015).
//
// The codec is intentionally minimal: ~600 lines across this package,
// zero external dependencies. owl runs under a 30 MB image cap and
// can't justify importing Prometheus's tsdb (which would pull a fat
// dependency tree). The algorithms are well-defined and don't change.
package chunkenc

// bstream is a bit-level append-only writer over a byte slice. Bits
// are packed left-to-right within each byte (MSB first), which matches
// the convention used in the Gorilla paper and Prometheus' tsdb.
type bstream struct {
	stream []byte
	// count is the number of bits remaining unused in the most-recent
	// byte of stream. It starts at 0 (no bytes yet) and reaches 0
	// again whenever a byte is freshly filled and a new one is about
	// to be appended.
	count uint8
}

// writeBit appends a single bit.
func (b *bstream) writeBit(bit bool) {
	if b.count == 0 {
		b.stream = append(b.stream, 0)
		b.count = 8
	}
	i := len(b.stream) - 1
	if bit {
		b.stream[i] |= 1 << (b.count - 1)
	}
	b.count--
}

// writeBits appends the low `nbits` bits of u, most significant first.
// nbits must be in [0, 64].
func (b *bstream) writeBits(u uint64, nbits int) {
	u <<= uint(64 - nbits)
	for nbits >= 8 {
		byt := byte(u >> 56)
		b.writeByte(byt)
		u <<= 8
		nbits -= 8
	}
	for nbits > 0 {
		b.writeBit(u&(1<<63) != 0)
		u <<= 1
		nbits--
	}
}

// writeByte is the fast path for byte-aligned writes. The aligned
// branch (count==8) lands the byte in one append; the unaligned
// branch splits across two bytes.
func (b *bstream) writeByte(byt byte) {
	if b.count == 0 {
		b.stream = append(b.stream, 0)
		b.count = 8
	}
	if b.count == 8 {
		b.stream[len(b.stream)-1] = byt
		b.count = 0
		return
	}
	i := len(b.stream) - 1
	// High `count` bits of byt go into the current byte's remaining
	// `count` slots; the low (8-count) bits start a new byte.
	b.stream[i] |= byt >> (8 - b.count)
	b.stream = append(b.stream, byt<<b.count)
}

// bytes returns the underlying buffer. The returned slice aliases the
// stream; callers that intend to keep it past the next Append must
// copy.
func (b *bstream) bytes() []byte { return b.stream }

// bstreamReader is the read counterpart of bstream. It reads bits
// left-to-right within each byte (MSB first).
type bstreamReader struct {
	stream []byte
	// idx points to the byte currently being consumed.
	idx int
	// count is how many bits remain unread in stream[idx]. Starts at 8
	// for a fresh byte; reaches 0 when the byte is exhausted and the
	// next read advances idx.
	count uint8
}

func newBReader(data []byte) bstreamReader {
	return bstreamReader{stream: data, idx: 0, count: 8}
}

// readBit consumes one bit, returning io-style ok=false at EOF.
func (b *bstreamReader) readBit() (bool, bool) {
	if b.idx >= len(b.stream) {
		return false, false
	}
	if b.count == 0 {
		b.idx++
		if b.idx >= len(b.stream) {
			return false, false
		}
		b.count = 8
	}
	bit := b.stream[b.idx]&(1<<(b.count-1)) != 0
	b.count--
	return bit, true
}

// readBits consumes `nbits` bits, returning them right-aligned in the
// low bits of the result.
func (b *bstreamReader) readBits(nbits int) (uint64, bool) {
	var u uint64
	for nbits >= 8 {
		byt, ok := b.readByte()
		if !ok {
			return 0, false
		}
		u = (u << 8) | uint64(byt)
		nbits -= 8
	}
	for nbits > 0 {
		bit, ok := b.readBit()
		if !ok {
			return 0, false
		}
		u <<= 1
		if bit {
			u |= 1
		}
		nbits--
	}
	return u, true
}

// readByte is the byte-aligned fast path.
func (b *bstreamReader) readByte() (byte, bool) {
	if b.idx >= len(b.stream) {
		return 0, false
	}
	if b.count == 0 {
		b.idx++
		if b.idx >= len(b.stream) {
			return 0, false
		}
		b.count = 8
	}
	if b.count == 8 {
		v := b.stream[b.idx]
		b.idx++
		b.count = 8
		return v, true
	}
	// Unaligned: take the remaining `count` high bits from stream[idx]
	// and the leading (8-count) bits from stream[idx+1].
	hi := b.stream[b.idx] << (8 - b.count)
	if b.idx+1 >= len(b.stream) {
		return 0, false
	}
	b.idx++
	lo := b.stream[b.idx] >> b.count
	return hi | lo, true
}

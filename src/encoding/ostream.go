package encoding

// Frked from "github.com/m3db/m3/src/dbnode/encoding/ostream.go" to make some changes that
// don't make sense to include upstream.

type Bit byte

// OStream encapsulates a writable stream.
type OStream interface {
	Len() int
	Empty() bool
	WriteBit(v Bit)
	WriteBits(v uint64, numBits int)
	WriteByte(v byte)
	WriteBytes(bytes []byte)
	Write(bytes []byte) (int, error)
	Reset(buffer []byte)
	Discard() []byte
	Rawbytes() ([]byte, int)
}

const (
	initAllocSize = 1024
)

// Ostream encapsulates a writable stream.
type ostream struct {
	buf []byte
	pos int // how many bits have been used in the last byte
}

// NewOStream creates a new Ostream
func NewOStream() OStream {
	return &ostream{}
}

// Len returns the length of the Ostream
func (os *ostream) Len() int {
	return len(os.buf)
}

// Empty returns whether the Ostream is empty
func (os *ostream) Empty() bool {
	return os.Len() == 0 && os.pos == 0
}

func (os *ostream) lastIndex() int {
	return os.Len() - 1
}

func (os *ostream) hasUnusedBits() bool {
	return os.pos > 0 && os.pos < 8
}

// grow appends the last byte of v to buf and sets pos to np.
func (os *ostream) grow(v byte, np int) {
	os.ensureCapacityFor(1)
	os.buf = append(os.buf, v)

	os.pos = np
}

// ensureCapacity ensures that there is at least capacity for n more bytes.
func (os *ostream) ensureCapacityFor(n int) {
	var (
		currCap      = cap(os.buf)
		currLen      = len(os.buf)
		availableCap = currCap - currLen
		missingCap   = n - availableCap
	)
	if missingCap <= 0 {
		// Already have enough capacity.
		return
	}

	newCap := max(cap(os.buf)*2, currCap+missingCap)
	newbuf := make([]byte, 0, newCap)
	newbuf = append(newbuf, os.buf...)
	os.buf = newbuf
}

func (os *ostream) fillUnused(v byte) {
	os.buf[os.lastIndex()] |= v >> uint(os.pos)
}

// WriteBit writes the last bit of v.
func (os *ostream) WriteBit(v Bit) {
	v <<= 7
	if !os.hasUnusedBits() {
		os.grow(byte(v), 1)
		return
	}
	os.fillUnused(byte(v))
	os.pos++
}

// WriteByte writes the last byte of v.
func (os *ostream) WriteByte(v byte) {
	if !os.hasUnusedBits() {
		os.grow(v, 8)
		return
	}
	os.fillUnused(v)
	os.grow(v<<uint(8-os.pos), os.pos)
}

// WriteBytes writes a byte slice.
func (os *ostream) WriteBytes(bytes []byte) {
	// Call ensureCapacityFor ahead of time to ensure that the bytes pool is used to
	// grow the buf (as opposed to append possibly triggering an allocation if
	// it wasn't) and that its only grown a maximum of one time regardless of the size
	// of the []byte being written.
	os.ensureCapacityFor(len(bytes))

	if !os.hasUnusedBits() {
		// If the stream is aligned on a byte boundary then all of the WriteByte()
		// function calls and bit-twiddling can be skipped in favor of a single
		// copy operation.
		os.buf = append(os.buf, bytes...)
		// Position 8 indicates that the last byte of the buffer has been completely
		// filled.
		os.pos = 8
		return
	}

	for i := 0; i < len(bytes); i++ {
		os.WriteByte(bytes[i])
	}
}

// Write writes a byte slice. This method exists in addition to WriteBytes()
// to satisfy the io.Writer interface.
func (os *ostream) Write(bytes []byte) (int, error) {
	os.WriteBytes(bytes)
	return len(bytes), nil
}

// WriteBits writes the lowest numBits of v to the stream, starting
// from the most significant bit to the least significant bit.
func (os *ostream) WriteBits(v uint64, numBits int) {
	if numBits == 0 {
		return
	}

	// we should never write more than 64 bits for a uint64
	if numBits > 64 {
		numBits = 64
	}

	v <<= uint(64 - numBits)
	for numBits >= 8 {
		os.WriteByte(byte(v >> 56))
		v <<= 8
		numBits -= 8
	}

	for numBits > 0 {
		os.WriteBit(Bit((v >> 63) & 1))
		v <<= 1
		numBits--
	}
}

// Discard takes the ref to the raw buffer from the ostream.
func (os *ostream) Discard() []byte {
	buffer := os.buf

	os.buf = nil
	os.pos = 0

	return buffer
}

// Reset resets the ostream
func (os *ostream) Reset(buffer []byte) {
	os.buf = buffer

	os.pos = 0
	if os.Len() > 0 {
		// If the byte array passed in is not empty, we set
		// pos to 8 indicating the last byte is fully used.
		os.pos = 8
	}
}

func (os *ostream) Rawbytes() ([]byte, int) {
	return os.buf, os.pos
}

func max(x, y int) int {
	if x > y {
		return x
	}
	return y
}

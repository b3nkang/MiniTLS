package tcpstack

func NewCircleBuf(maxSize uint32, baseSeq uint32) *CircleBuf {
    return &CircleBuf {
        buf: make([]byte, MAX_WIN_SIZE),
        currSize: 0,
        maxSize: MAX_WIN_SIZE,
        baseSeq: baseSeq,
        head: 0,
    }
}

func (cb *CircleBuf) SeqNumToIndex(seqNum uint32) int {
	offset := int(seqNum - cb.baseSeq)
	return (cb.head + offset) % int(cb.maxSize)
}

func (cb *CircleBuf) FreeSpace() uint32 {
    return cb.maxSize - cb.currSize
}

func (cb *CircleBuf) IsEmpty() bool {
    return cb.currSize == 0
}

func (cb *CircleBuf) WriteIntoBuf(seqNum uint32, data []byte) int {
    if len(data) == 0 {
        return 0
    }

    start := cb.SeqNumToIndex(seqNum)
    firstChunk := min(len(data), len(cb.buf)-start) // write either the whole block of data or hit the end and wrap around

    copy(cb.buf[start:start+firstChunk], data[:firstChunk])

    remaining := len(data) - firstChunk
    if remaining > 0 { // wrap around with the rest
        copy(cb.buf[0:remaining], data[firstChunk:])
    }

    cb.currSize += uint32(len(data)) // TODO: may need to update this line after mstone2
    return len(data)
}

func (cb *CircleBuf) ReadFromBuf(seqNum uint32, dest []byte, n uint32) int {
    if n < 1 || len(dest) == 0 {
		return 0
	}
	if n > uint32(len(dest)) {
		n = uint32(len(dest))
	}

    start := cb.SeqNumToIndex(seqNum)
    firstChunk := min(int(n), len(cb.buf)-start) // read either the whole block of data or hit the end and wrap around

	copy(dest[:firstChunk], cb.buf[start:start+firstChunk])

    remaining := int(n) - firstChunk
    if remaining > 0 { // wrap around with the rest
		copy(dest[firstChunk:firstChunk+remaining], cb.buf[0:remaining])
    }

    return int(n)
}

func (cb *CircleBuf) SliceFrom(seq uint32, n uint32) []byte {
	if n == 0 {
		return make([]byte, 0)
	}

	out := make([]byte, n)
	cb.ReadFromBuf(seq, out, n)
	return out
}

func (cb *CircleBuf) AdvanceBase(n uint32) {
	if n == 0 {
		return
	}
	if n > cb.currSize {
		n = cb.currSize
	}
	cb.baseSeq += n
	cb.currSize -= n
	cb.head = (cb.head + int(n)) % int(cb.maxSize)
}
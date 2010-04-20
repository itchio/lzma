package lzma

import (
	"io"
	//"os"
)

type lzInWindow struct {
	r                io.Reader
	buf              []byte
	posLimit         uint32
	ptrToLastSafePos uint32
	bufOffset        int32
	blockSize        uint32
	pos              uint32
	keepSizeBefore   uint32
	keepSizeAfter    uint32
	streamPos        uint32
	streamEndReached bool
}

func (iw *lzInWindow) moveBlock() {
	offset := int32(iw.bufOffset + int32(iw.pos) - int32(iw.keepSizeBefore))
	if offset > 0 {
		offset--
	}
	numBytes := int32(iw.bufOffset + int32(iw.streamPos) - int32(offset))
	for i := int32(0); i < numBytes; i++ {
		iw.buf[i] = iw.buf[offset+i]
	}
	iw.bufOffset -= offset
}

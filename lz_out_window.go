package lzma

import (
	"io"
	"os"
)

type lzOutWindow struct {
	w         io.Writer
	buf       []byte
	winSize   int
	pos       int
	streamPos int
}

func newLzOutWindow(w io.Writer, windowSize int) (ow lzOutWindow) {
	ow.w = w
	ow.buf = make([]byte, windowSize)
	ow.winSize = windowSize
	ow.pos = 0
	ow.streamPos = 0
	return
}

func (ow *lzOutWindow) flush() (err os.Error) {
	size := ow.pos - ow.streamPos
	if size == 0 {
		return
	}
	n, err := ow.w.Write(ow.buf[ow.streamPos:ow.streamPos+size])
	if err != nil {
		return
	}
	if n != size {
		return os.NewError("expected to write " + string(size) + " bytes, written " + string(n) + " bytes")
	}
	if ow.pos >= ow.winSize {
		ow.pos = 0
	}
	ow.streamPos = ow.pos
	return
}

func (ow *lzOutWindow) copyBlock(distance int, length int) (err os.Error) {
	pos := ow.pos - distance - 1
	if pos < 0 {
		pos += ow.winSize
	}
	for ;length != 0; length-- {
		if pos >= ow.winSize {
			pos = 0
		}
		ow.pos++
		pos++
		ow.buf[ow.pos] = ow.buf[pos]
		if ow.pos >= ow.winSize {
			if err = ow.flush(); err != nil {
				return
			}
		}
	}
	return
}

func (ow *lzOutWindow) putByte(b byte) (err os.Error) {
	ow.pos++
	ow.buf[ow.pos] = b
	if ow.pos > ow.winSize {
		if err = ow.flush(); err != nil {
			return
		}
	}
	return
}

func (ow *lzOutWindow) getByte(distance int) (b byte) {
	pos := ow.pos - distance - 1
	if pos < 0 {
		pos += ow.winSize
	}
	b = ow.buf[pos]
	return
}

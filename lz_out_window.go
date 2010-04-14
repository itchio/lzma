package lzma

import (
	"io"
	"os"
)

type lzOutWindow struct {
	w         io.Writer
	buf       []byte
	winSize   uint32
	pos       uint32
	streamPos uint32
}

func newLzOutWindow(w io.Writer, windowSize uint32) *lzOutWindow {
	return &lzOutWindow{
		w:         w,
		buf:       make([]byte, windowSize),
		winSize:   windowSize,
		pos:       0,
		streamPos: 0,
	}
}

func (outWin *lzOutWindow) flush() (err os.Error) {
	size := outWin.pos - outWin.streamPos
	if size == 0 {
		return
	}
	n, err := outWin.w.Write(outWin.buf[outWin.streamPos : outWin.streamPos+size])
	if err != nil {
		return
	}
	if uint32(n) != size {
		return os.NewError("expected to write " + string(size) + " bytes, written " + string(n) + " bytes")
	}
	if outWin.pos >= outWin.winSize {
		outWin.pos = 0
	}
	outWin.streamPos = outWin.pos
	return
}

func (outWin *lzOutWindow) copyBlock(distance, length uint32) (err os.Error) {
	pos := int32(outWin.pos) - int32(distance) - 1
	if pos < 0 {
		pos += int32(outWin.winSize)
	}
	for ; length != 0; length-- {
		if pos >= int32(outWin.winSize) {
			pos = 0
		}
		outWin.pos++
		pos++
		outWin.buf[outWin.pos] = outWin.buf[pos]
		if outWin.pos >= outWin.winSize {
			if err = outWin.flush(); err != nil {
				return
			}
		}
	}
	return
}

func (outWin *lzOutWindow) putByte(b byte) (err os.Error) {
	outWin.pos++
	outWin.buf[outWin.pos] = b
	if outWin.pos > outWin.winSize {
		if err = outWin.flush(); err != nil {
			return
		}
	}
	return
}

func (outWin *lzOutWindow) getByte(distance uint32) (b byte) {
	pos := int32(outWin.pos) - int32(distance) - 1
	if pos < 0 {
		pos += int32(outWin.winSize)
	}
	b = outWin.buf[pos]
	return
}

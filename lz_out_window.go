package lzma

import (
	"io"
	"os"
	//"fmt"
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
	pos := int32(int32(outWin.pos) - int32(distance) - 1)
	if pos < 0 {
		pos += int32(outWin.winSize)
	}
	for ; length != 0; length-- {
		if pos >= int32(outWin.winSize) {
			pos = 0
		}
		//fmt.Printf("outWin.copyBlock(), before buf: outWin.pos = %d, outWin.buf[outWin.pos] = %d, pos = %d\n", outWin.pos, outWin.buf[outWin.pos], pos)
		outWin.buf[outWin.pos] = outWin.buf[pos]
		outWin.pos++
		pos++
		//fmt.Printf("outWin.copyBlock(), after  buf: outWin.pos = %d, outWin.buf[outWin.pos] = %d, pos = %d\n", outWin.pos, outWin.buf[outWin.pos], pos)
		if outWin.pos >= outWin.winSize {
			if err = outWin.flush(); err != nil {
				return
			}
		}
		//fmt.Printf("outWin.copyBlock(): distance = %d, len = %d, pos = %d, outWin.pos = %d, outWin.winSize = %d, " +
		//			"outWin.buf[outWin.pos] = %d\n", distance, length, pos, outWin.pos, outWin.winSize, outWin.buf[outWin.pos])
	}
	return
}

func (outWin *lzOutWindow) putByte(b byte) (err os.Error) {
	//if (outWin.winSize - outWin.pos) < 10 {
	//	fmt.Printf("outWin.putByte(): b = %d, outWin.pos = %d, outWin.winSize = %d, len(outWin.buf) = %d\n", b, outWin.pos, outWin.winSize, len(outWin.buf))
	//}
	outWin.buf[outWin.pos] = b
	outWin.pos++
	//fmt.Printf("outWin.putByte(): b = %d, outWin.pos = %d, outWin.winSize = %d, len(outWin.buf) = %d\n", b, outWin.pos, outWin.winSize, len(outWin.buf))
	if outWin.pos >= outWin.winSize {
		err = outWin.flush()
		if err != nil {
			return
		}
	}
	return
}

func (outWin *lzOutWindow) getByte(distance uint32) (b byte) {
	pos := int32(int32(outWin.pos) - int32(distance) - 1)
	if pos < 0 {
		pos += int32(outWin.winSize)
	}
	b = outWin.buf[pos]
	//fmt.Printf("outWin.getByte(): distance = %d, pos = %d, outWin.pos = %d, winSize = %d, b = %d, len(buf) = %d, outWin.streamPos = %d\n", 
	//		distance, pos, outWin.pos, outWin.winSize, b, len(outWin.buf), outWin.streamPos)
	return
}

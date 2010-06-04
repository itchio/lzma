// Copyright (c) 2010, Andrei Vieru. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The lzma package implements reading and writing of LZMA format compressed data.
// Reference implementation is LZMA SDK version 4.65 originaly developed by Igor
// Pavlov, available online at:
//
//  http://www.7-zip.org/sdk.html
//
package lzma

import (
	"io"
	"os"
)

const (
	inBufSize           = 1 << 16
	outBufSize          = 1 << 16
	lzmaPropSize        = 5
	lzmaHeaderSize      = lzmaPropSize + 8
	lzmaMaxReqInputSize = 20

	kNumRepDistances                = 4
	kNumStates                      = 12
	kNumPosSlotBits                 = 6
	kDicLogSizeMin                  = 0
	kNumLenToPosStatesBits          = 2
	kNumLenToPosStates              = 1 << kNumLenToPosStatesBits
	kMatchMinLen                    = 2
	kNumAlignBits                   = 4
	kAlignTableSize                 = 1 << kNumAlignBits
	kAlignMask                      = kAlignTableSize - 1
	kStartPosModelIndex             = 4
	kEndPosModelIndex               = 14
	kNumPosModels                   = kEndPosModelIndex - kStartPosModelIndex
	kNumFullDistances               = 1 << (kEndPosModelIndex / 2)
	kNumLitPosStatesBitsEncodingMax = 4
	kNumLitContextBitsMax           = 8
	kNumPosStatesBitsMax            = 4
	kNumPosStatesMax                = 1 << kNumPosStatesBitsMax
	kNumLowLenBits                  = 3
	kNumMidLenBits                  = 3
	kNumHighLenBits                 = 8
	kNumLowLenSymbols               = 1 << kNumLowLenBits
	kNumMidLenSymbols               = 1 << kNumMidLenBits
	kNumLenSymbols                  = kNumLowLenSymbols + kNumMidLenSymbols + (1 << kNumHighLenBits)
	kMatchMaxLen                    = kMatchMinLen + kNumLenSymbols - 1
)


// A streamError reports the presence of corrupt input stream.
var streamError os.Error = os.NewError("error in lzma encoded data stream")

// A headerError reports an error in the header of the lzma encoder file.
var headerError os.Error = os.NewError("error in lzma header")

// A nReadError reports what it's message reads
var nReadError os.Error = os.NewError("number of bytes returned by Reader.Read() didn't meet expectances")

// A nWriteError reports what it's message reads
var nWriteError os.Error = os.NewError("number of bytes returned by Writer.Write() didn't meet expectances")


func stateUpdateChar(index uint32) uint32 {
	if index < 4 {
		return 0
	}
	if index < 10 {
		return index - 3
	}
	return index - 6
}

func stateUpdateMatch(index uint32) uint32 {
	if index < 7 {
		return 7
	}
	return 10
}

func stateUpdateRep(index uint32) uint32 {
	if index < 7 {
		return 8
	}
	return 11
}

func stateUpdateShortRep(index uint32) uint32 {
	if index < 7 {
		return 9
	}
	return 11
}

func stateIsCharState(index uint32) bool {
	if index < 7 {
		return true
	}
	return false
}

func getLenToPosState(length uint32) uint32 {
	length -= kMatchMinLen
	if length < kNumLenToPosStates {
		return length
	}
	return kNumLenToPosStates - 1
}


// LZMA compressed file format
// ---------------------------
// Offset Size 	      Description
//   0     1   		Special LZMA properties (lc,lp, pb in encoded form)
//   1     4   		Dictionary size (little endian)
//   5     8   		Uncompressed size (little endian). Size -1 stands for unknown size


// lzma properties
type props struct {
	lc, lp, pb uint8
	dictSize   uint32
}

func (p *props) decodeProps(buf []byte) {
	d := buf[0]
	if d > (9 * 5 * 5) {
		error(headerError) // panic, will recover later
	}
	p.lc = d % 9
	d /= 9
	p.pb = d / 5
	p.lp = d % 5
	if p.lc > kNumLitContextBitsMax || p.lp > 4 || p.pb > kNumPosStatesBitsMax {
		error(headerError) // panic, will recover later
	}
	for i := 0; i < 4; i++ {
		p.dictSize += uint32(buf[i+1]) << uint32(i*8)
	}
}


type decoder struct {
	// i/o
	rd     *rangeDecoder // r
	outWin *lzOutWindow  // w

	// lzma header
	prop       *props
	unpackSize int64

	// hz
	matchDecoders    []uint16
	repDecoders      []uint16
	repG0Decoders    []uint16
	repG1Decoders    []uint16
	repG2Decoders    []uint16
	rep0LongDecoders []uint16
	posSlotCoders    []*rangeBitTreeCoder
	posDecoders      []uint16
	posAlignCoder    *rangeBitTreeCoder
	lenCoder         *lenCoder
	repLenCoder      *lenCoder
	litCoder         *litCoder
	dictSizeCheck    uint32
	posStateMask     uint32
}

func (z *decoder) doDecode() {
	var state uint32 = 0
	var rep0 uint32 = 0
	var rep1 uint32 = 0
	var rep2 uint32 = 0
	var rep3 uint32 = 0
	var nowPos uint64 = 0
	var prevByte byte = 0

	for z.unpackSize < 0 || int64(nowPos) < z.unpackSize {
		posState := uint32(nowPos) & z.posStateMask
		res := z.rd.decodeBit(z.matchDecoders, state<<kNumPosStatesBitsMax+posState)
		if res == 0 {
			lsc := z.litCoder.getSubCoder(uint32(nowPos), prevByte)
			if !stateIsCharState(state) {
				res := lsc.decodeWithMatchByte(z.rd, z.outWin.getByte(rep0))
				prevByte = res
			} else {
				res := lsc.decodeNormal(z.rd)
				prevByte = res
			}
			z.outWin.putByte(prevByte)
			state = stateUpdateChar(state)
			nowPos++
		} else {
			var length uint32
			res := z.rd.decodeBit(z.repDecoders, state)
			if res == 1 {
				length = 0
				res := z.rd.decodeBit(z.repG0Decoders, state)
				if res == 0 {
					res := z.rd.decodeBit(z.rep0LongDecoders, state<<kNumPosStatesBitsMax+posState)
					if res == 0 {
						state = stateUpdateShortRep(state)
						length = 1
					}
				} else {
					var distance uint32
					res := z.rd.decodeBit(z.repG1Decoders, state)
					if res == 0 {
						distance = rep1
					} else {
						res := z.rd.decodeBit(z.repG2Decoders, state)
						if res == 0 {
							distance = rep2
						} else {
							distance = rep3
							rep3 = rep2
						}
						rep2 = rep1
					}
					rep1 = rep0
					rep0 = distance
				}
				if length == 0 {
					res := z.repLenCoder.decode(z.rd, posState)
					length = res + kMatchMinLen
					state = stateUpdateRep(state)
				}
			} else {
				rep3 = rep2
				rep2 = rep1
				rep1 = rep0
				res := z.lenCoder.decode(z.rd, posState)
				length = res + kMatchMinLen
				state = stateUpdateMatch(state)
				posSlot := z.posSlotCoders[getLenToPosState(length)].decode(z.rd)
				if posSlot >= kStartPosModelIndex {
					numDirectBits := posSlot>>1 - 1
					rep0 = (2 | posSlot&1) << numDirectBits
					if posSlot < kEndPosModelIndex {
						res := reverseDecodeIndex(z.rd, z.posDecoders, rep0-posSlot-1, numDirectBits)
						rep0 += res
					} else {
						res := z.rd.decodeDirectBits(numDirectBits - kNumAlignBits)
						rep0 += res << kNumAlignBits
						res = z.posAlignCoder.reverseDecode(z.rd)
						rep0 += res
						if int32(rep0) < 0 {
							if rep0 == 0xFFFFFFFF {
								break
							}
							error(streamError) // panic, will recover later
						}
					}
				} else {
					rep0 = posSlot
				}
			}
			if uint64(rep0) >= nowPos || rep0 >= z.dictSizeCheck {
				error(streamError) // panic, will recover later
			}
			z.outWin.copyBlock(rep0, length)
			nowPos += uint64(length)
			prevByte = z.outWin.getByte(0)
		}
	}
	z.outWin.flush()
}

func (z *decoder) decoder(r io.Reader, w io.Writer) (err os.Error) {
	defer handlePanics(&err)

	// read 13 bytes (lzma header)
	header := make([]byte, lzmaHeaderSize)
	n, err := r.Read(header) // ERR
	if err != nil {
		return
	}
	if n != lzmaHeaderSize {
		return nReadError
	}
	z.prop = &props{}
	z.prop.decodeProps(header)

	z.unpackSize = 0
	for i := 0; i < 8; i++ {
		b := header[lzmaPropSize+i]
		z.unpackSize = z.unpackSize | int64(b)<<uint64(8*i)
	}

	// do not move before r.Read(header)
	z.rd = newRangeDecoder(r)

	z.dictSizeCheck = maxUInt32(z.prop.dictSize, 1)
	z.outWin = newLzOutWindow(w, maxUInt32(z.dictSizeCheck, 1<<12))

	z.litCoder = newLitCoder(uint32(z.prop.lp), uint32(z.prop.lc))
	z.lenCoder = newLenCoder(uint32(1 << z.prop.pb))
	z.repLenCoder = newLenCoder(uint32(1 << z.prop.pb))
	z.posStateMask = uint32(1<<z.prop.pb - 1)
	z.matchDecoders = initBitModels(kNumStates << kNumPosStatesBitsMax)
	z.repDecoders = initBitModels(kNumStates)
	z.repG0Decoders = initBitModels(kNumStates)
	z.repG1Decoders = initBitModels(kNumStates)
	z.repG2Decoders = initBitModels(kNumStates)
	z.rep0LongDecoders = initBitModels(kNumStates << kNumPosStatesBitsMax)
	z.posDecoders = initBitModels(kNumFullDistances - kEndPosModelIndex)
	z.posSlotCoders = make([]*rangeBitTreeCoder, kNumLenToPosStates)
	for i := 0; i < kNumLenToPosStates; i++ {
		z.posSlotCoders[i] = newRangeBitTreeCoder(kNumPosSlotBits)
	}
	z.posAlignCoder = newRangeBitTreeCoder(kNumAlignBits)

	z.doDecode()
	return
}

// NewDecoder returns a new ReadCloser that can be used to read the uncompressed
// version of r. It is the caller's responsibility to call Close on the ReadCloser
// when finished reading.
func NewDecoder(r io.Reader) io.ReadCloser {
	var z decoder
	pr, pw := io.Pipe()
	go func() {
		err := z.decoder(r, pw)
		pw.CloseWithError(err)
	}()
	return pr
}

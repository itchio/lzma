/*
The lzma package implements reading and writing of LZMA
format compressed data originaly developed by Igor Pavlov.
As reference implementations have been taken LZMA SDK version 4.65
available online at:

  http://www.7-zip.org/sdk.html

Note that LZMA doesn't store any metadata about the file. Neither can
it compress multiple files because it's not an archiving format. Both
these issues are solved if the file or files are archived with tar
before compression with LZMA.
*/
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
//   5     8   		Uncompressed size (little endian). Size -1 means unknown size
//  13     5		range coder's code field
// end-6   6		End Marker Bytes, only if size == -1


// lzma properties
type props struct {
	lc, lp, pb uint8
	dictSize   uint32
}

func (p *props) decodeProps(buf []byte) (err os.Error) {
	d := buf[0]
	if d > (9 * 5 * 5) {
		return os.NewError("illegal value of encoded lc, lp, pb byte " + string(d))
	}
	p.lc = d % 9
	d /= 9
	p.pb = d / 5
	p.lp = d % 5
	if p.lc > kNumLitContextBitsMax || p.lp > 4 || p.pb > kNumPosStatesBitsMax {
		return os.NewError("illegal values of lc, lp or pb: " + string(p.lc) + ", " + string(p.lp) + ", " + string(p.pb))
	}
	for i := 0; i < 4; i++ {
		p.dictSize += uint32(buf[i+1]&0xff) << uint32(i*8)
	}
	return
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

func (z *decoder) doDecode() (err os.Error) {
	var state uint32 = 0
	var rep0 int32 = 0
	var rep1 int32 = 0
	var rep2 int32 = 0
	var rep3 int32 = 0
	var nowPos int64 = 0
	var prevByte byte = 0

	for z.unpackSize < 0 || nowPos < z.unpackSize {
		posState := uint32(nowPos) & z.posStateMask
		if res, err := z.rd.decodeBit(z.matchDecoders, state<<kNumPosStatesBitsMax+posState); err != nil {
			return
		} else if res == 0 {
			lc2 := z.litCoder.getCoder(uint32(nowPos), prevByte)
			if !stateIsCharState(state) {
				res, err := lc2.decodeWithMatchByte(z.rd, z.outWin.getByte(uint32(rep0)))
				if err != nil {
					return
				}
				prevByte = byte(res)
			} else {
				res, err := lc2.decodeNormal(z.rd)
				if err != nil {
					return
				}
				prevByte = byte(res)
			}
			err := z.outWin.putByte(prevByte)
			if err != nil {
				return
			}
			state = stateUpdateChar(state)
			nowPos++
		} else {
			var length uint32
			if res, err := z.rd.decodeBit(z.repDecoders, state); err != nil {
				return
			} else if res == 1 {
				length = 0
				if res, err := z.rd.decodeBit(z.repG0Decoders, state); err != nil {
					return
				} else if res == 0 {
					if res, err := z.rd.decodeBit(z.rep0LongDecoders, state<<kNumPosStatesBitsMax+posState); err != nil {
						return
					} else if res == 0 {
						state = stateUpdateShortRep(state)
						length = 1
					}
				} else {
					var distance int32
					if res, err := z.rd.decodeBit(z.repG1Decoders, state); err != nil {
						return
					} else if res == 0 {
						distance = rep1
					} else {
						if res, err := z.rd.decodeBit(z.repG2Decoders, state); err != nil {
							return
						} else if res == 0 {
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
					res, err := z.repLenCoder.decode(z.rd, posState)
					if err != nil {
						return
					}
					length = res + kMatchMinLen
					state = stateUpdateRep(state)
				}
			} else {
				rep3 = rep2
				rep2 = rep1
				rep1 = rep0
				res, err := z.lenCoder.decode(z.rd, posState)
				if err != nil {
					return
				}
				length = res + kMatchMinLen
				state = stateUpdateMatch(state)
				posSlot, err := z.posSlotCoders[getLenToPosState(length)].decode(z.rd)
				if err != nil {
					return
				}
				if posSlot >= kStartPosModelIndex {
					numDirectBits := uint32(posSlot>>1 - 1)
					rep0 = int32((2 | posSlot&1) << numDirectBits)
					if posSlot < kEndPosModelIndex {
						res, err := reverseDecodeIndex(z.rd, z.posDecoders, rep0-int32(posSlot)-1, numDirectBits)
						if err != nil {
							return
						}
						rep0 += int32(res)
					} else {
						res, err := z.rd.decodeDirectBits(numDirectBits - kNumAlignBits)
						if err != nil {
							return
						}
						rep0 += int32(res << kNumAlignBits)
						res, err = z.posAlignCoder.reverseDecode(z.rd)
						if err != nil {
							return
						}
						rep0 += int32(res)
						if rep0 < 0 {
							if rep0 == -1 {
								break
							}
							return os.NewError("error in data stream (checkpoint 1)")
						}
					}
				} else {
					rep0 = int32(posSlot)
				}
			}
			if int64(rep0) >= nowPos || rep0 >= int32(z.dictSizeCheck) {
				return os.NewError("error in data stream (checkpoint 2)")
			}
			if err := z.outWin.copyBlock(uint32(rep0), length); err != nil {
				return
			}
			nowPos += int64(length)
			prevByte = z.outWin.getByte(0)
		}
	}
	if err := z.outWin.flush(); err != nil {
		return
	}
	return nil
}

func (z *decoder) decoder(r io.Reader, w io.Writer) (err os.Error) {
	// read first 13 bytes from r which contain lc, lp, pb, dictSize and unpackedSize
	header := make([]byte, lzmaHeaderSize)
	n, err := r.Read(header)
	if err != nil {
		return
	}
	if n != lzmaHeaderSize {
		return os.NewError("read " + string(n) + " bytes instead of " + string(lzmaHeaderSize))
	}
	z.prop = &props{}
	if err = z.prop.decodeProps(header); err != nil {
		return
	}

	z.unpackSize = 0
	for i := 0; i < 8; i++ {
		b := header[lzmaPropSize+i]
		if int32(b) < 0 {
			return os.NewError("can't read stream size")
		}
		z.unpackSize = z.unpackSize | int64(b)<<uint64(8*i)
	}

	// do not move the initialization of z.rd before that of z.prop and z.unpackSize
	z.rd, err = newRangeDecoder(r)
	if err != nil {
		return
	}

	if z.prop.dictSize >= 1 {
		z.dictSizeCheck = z.prop.dictSize
	} else {
		z.dictSizeCheck = 1
	}
	if z.dictSizeCheck >= 1<<12 {
		z.outWin = newLzOutWindow(w, z.dictSizeCheck)
	} else {
		z.outWin = newLzOutWindow(w, 1<<12)
	}
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

	err = z.doDecode()
	return
}

func NewDecoder(r io.Reader) io.ReadCloser {
	var z decoder
	pr, pw := io.Pipe()
	go func() {
		err := z.decoder(r, pw)
		pw.CloseWithError(err)
	}()
	return pr
}

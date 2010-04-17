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
	//"fmt"
)

const (
	inBufSize           = 1 << 16
	outBufSize          = 1 << 16
	lzmaPropSize        = 5
	lzmaHeaderSize      = lzmaPropSize + 8
	lzmaMaxReqInputSize = 20
)

// LZMA compressed file format
// ---------------------------
// Offset Size Description
//   0     1   Special LZMA properties (lc,lp, pb in encoded form)
//   1     4   Dictionary size (little endian)
//   5     8   Uncompressed size (little endian). -1 means unknown size
//  13         Compressed data

// lzma pproperties
type props struct {
	lc, lp, pb uint8
	dictSize   uint32
}

type decoder struct { // flate.inflater, zlib.reader, gzip.inflater
	// input sources
	rd *rangeDecoder
	w  io.Writer

	// lzma header
	prop       *props
	unpackSize int64

	// hz
	outWin *lzOutWindow

	// hz
	matchDecoders    []uint16
	repDecoders      []uint16
	repG0Decoders    []uint16
	repG1Decoders    []uint16
	repG2Decoders    []uint16
	rep0LongDecoders []uint16
	posSlotDecoders  []*rangeBitTreeDecoder
	posDecoders      []uint16
	posAlignDecoder  *rangeBitTreeDecoder
	lenDecoder       *lenDecoder
	repLenDecoder    *lenDecoder
	litDecoder       *litDecoder
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
		//fmt.Printf("lzma.decoder.doDecode(), beggining of main loop: nowPos = %d, unpackSize = %d\n", nowPos, z.unpackSize)
		posState := uint32(nowPos) & z.posStateMask
		if res, err := z.rd.decodeBit(z.matchDecoders, state<<kNumPosStatesBitsMax+posState); err != nil {
			return
		} else if res == 0 {
			//fmt.Printf(" result from RD.Decoder.decodeBit(): res = %d\n", res)
			ld2 := z.litDecoder.getDecoder(uint32(nowPos), prevByte)
			if !stateIsCharState(state) {
				//fmt.Printf("lzma.decoder.doDecode() before ld2.decodeWithMatchByte(): rep0 = %d\n", rep0)
				res, err := ld2.decodeWithMatchByte(z.rd, z.outWin.getByte(uint32(rep0)))
				if err != nil {
					return
				}
				prevByte = byte(res)
			} else {
				res, err := ld2.decodeNormal(z.rd)
				if err != nil {
					return
				}
				prevByte = byte(res)
				//fmt.Printf("litDecoder.decodeNormal(): res = %d\n", prevByte)
			}
			err := z.outWin.putByte(prevByte)
			if err != nil {
				return
			}
			state = stateUpdateChar(state)
			nowPos++
		} else {
			//fmt.Printf(" result from RD.Decoder.decodeBit(): res = %d\n", res)
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
					res, err := z.repLenDecoder.decode(z.rd, posState)
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
				res, err := z.lenDecoder.decode(z.rd, posState)
				if err != nil {
					return
				}
				length = res + kMatchMinLen
				state = stateUpdateMatch(state)
				posSlot, err := z.posSlotDecoders[getLenToPosState(length)].decode(z.rd)
				if err != nil {
					return
				}
				if posSlot >= kStartPosModelIndex {
					numDirectBits := uint32(posSlot>>1 - 1)
					rep0 = int32((2 | posSlot&1) << numDirectBits)
					//fmt.Printf("lzma.decoder.doDecode(), inside of main loop: posSlot = %d, numDirectBits = %d, " +
					//			"rep0 = %d\n", posSlot, numDirectBits, rep0)
					if posSlot < kEndPosModelIndex {
						res, err := reverseDecodeIndex(z.rd, z.posDecoders, rep0-int32(posSlot)-1, numDirectBits)
						if err != nil {
							return
						}
						rep0 += int32(res)
						//fmt.Printf("lzma.decoder.doDecode(), inside of main loop [0]: posSlot = %d, numDirectBits = %d, " +
						//		"rep0 = %d\n", posSlot, numDirectBits, rep0)
					} else {
						//fmt.Printf("lzma.decoder.doDecode(), inside of main loop [1]: posSlot = %d, numDirectBits = %d, " +
						//                "rep0 = %d\n", posSlot, numDirectBits, rep0)
						res, err := z.rd.decodeDirectBits(numDirectBits - kNumAlignBits)
						if err != nil {
							return
						}
						//fmt.Printf("lzma.decoder.doDecode(), inside of main loop [2]: posSlot = %d, numDirectBits = %d, " +
						//                "rep0 = %d, res = %d\n", posSlot, numDirectBits, rep0, res)
						rep0 += int32(res << kNumAlignBits)
						//fmt.Printf("lzma.decoder.doDecode(), inside of main loop [3]: posSlot = %d, numDirectBits = %d, " +
						//                "rep0 = %d\n", posSlot, numDirectBits, rep0)
						res, err = z.posAlignDecoder.reverseDecode(z.rd)
						if err != nil {
							return
						}
						//fmt.Printf("lzma.decoder.doDecode(), inside of main loop [4]: posSlot = %d, numDirectBits = %d, " +
						//                "rep0 = %d, res = %d\n", posSlot, numDirectBits, rep0, res)
						rep0 += int32(res)
						//fmt.Printf("lzma.decoder.doDecode(), inside of main loop [5]: posSlot = %d, numDirectBits = %d, " +
						//                "rep0 = %d\n", posSlot, numDirectBits, rep0)
						if rep0 < 0 {
							if rep0 == -1 {
								//fmt.Printf("lzma.decoder.doDecode() rep0 == -1, break here\n")
								break
							}
							return os.NewError("error in data stream (checkpoint 1)")
						}
					}
				} else {
					rep0 = int32(posSlot)
				}
			}
			//fmt.Printf("lzma.decoder.doDecode(): rep0 = %d, nowPos = %d, z.dictSizeCheck = %d\n", rep0, nowPos, z.dictSizeCheck)
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

func (z *decoder) decodeProps(buf []byte) (err os.Error) {
	d := buf[0]
	if d > (9 * 5 * 5) {
		return os.NewError("illegal value of encoded lc, lp, pb byte " + string(d))
	}
	z.prop.lc = d % 9
	d /= 9
	z.prop.pb = d / 5
	z.prop.lp = d % 5
	if z.prop.lc > kNumLitContextBitsMax || z.prop.lp > 4 || z.prop.pb > kNumPosStatesBitsMax {
		return os.NewError("illegal values of lc, lp or pb: " + string(z.prop.lc) + ", " + string(z.prop.lp) + ", " + string(z.prop.pb))
	}
	//z.prop.dictSize = uint32(buf[1]) | uint32(buf[2]<<8) | uint32(buf[3]<<16) | uint32(buf[4]<<24)
	for i := 0; i < 4; i++ {
		z.prop.dictSize += uint32(buf[i+1]&0xff) << uint32(i*8)
	}
	//fmt.Printf("lzma.decoder.decodeProps(): z.prop.dictSize = %d, z.prop.lc = %d, z.prop.lp = %d, z.prop.pb = %d\n", z.prop.dictSize, z.prop.lc, z.prop.lp, z.prop.pb)
	return
}

// decoder initializes a decoder; it reads first 13 bytes from r which contain
// lc, lp, pb, dictSize and unpackedSize; next creates a rangeDecoder; the
// rangeDecoder should be created after lzmaHeader is read from r because
// newRangeDecoder() further reads from the same stream 5 bytes to
// init rangeDecoder.code
func (z *decoder) decoder(r io.Reader, w io.Writer) (err os.Error) {
	// init z

	// z.w
	z.w = w

	// z.prop
	header := make([]byte, lzmaHeaderSize)
	n, err := r.Read(header)
	if err != nil {
		return
	}
	if n != lzmaHeaderSize {
		return os.NewError("read " + string(n) + " bytes instead of " + string(lzmaHeaderSize))
	}
	z.prop = &props{}
	if err = z.decodeProps(header); err != nil {
		return
	}

	// z.unpackSize
	z.unpackSize = 0
	for i := 0; i < 8; i++ {
		b := header[lzmaPropSize+i]
		if int32(b) < 0 {
			return os.NewError("can't read stream size")
		}
		z.unpackSize = z.unpackSize | int64(b)<<uint64(8*i)
	}
	//fmt.Printf("lzma.decoder.decoder(): z.unpackSize = %d\n", z.unpackSize)

	// z.rd	// do not move before z.prop
	z.rd, err = newRangeDecoder(r)
	if err != nil {
		return
	}

	// z.dictSizeCheck
	if z.prop.dictSize >= 1 {
		z.dictSizeCheck = z.prop.dictSize
	} else {
		z.dictSizeCheck = 1
	}

	// z.outWin
	if z.dictSizeCheck >= 1<<12 {
		z.outWin = newLzOutWindow(z.w, z.dictSizeCheck) // z.w ?
	} else {
		z.outWin = newLzOutWindow(z.w, 1<<12) // z.w ?
	}

	// z.litDecoder
	z.litDecoder = newLitDecoder(uint32(z.prop.lp), uint32(z.prop.lc))

	// z.lenDecoder
	z.lenDecoder = newLenDecoder(uint32(1 << z.prop.pb))

	// z.repLenDecoder
	z.repLenDecoder = newLenDecoder(uint32(1 << z.prop.pb))

	// z.posStateMask
	z.posStateMask = uint32(1<<z.prop.pb - 1)

	// z.matchDecoders
	z.matchDecoders = initBitModels(kNumStates << kNumPosStatesBitsMax)

	// z.repDecoders
	z.repDecoders = initBitModels(kNumStates)

	// z.repG0Decoders
	z.repG0Decoders = initBitModels(kNumStates)

	// z.rep10Decoders
	z.repG1Decoders = initBitModels(kNumStates)

	// z.repG2Decoders
	z.repG2Decoders = initBitModels(kNumStates)

	// z.rep0LongDecoders
	z.rep0LongDecoders = initBitModels(kNumStates << kNumPosStatesBitsMax)

	// z.posDecoders
	z.posDecoders = initBitModels(kNumFullDistances - kEndPosModelIndex)

	// z.posSlotDecoders
	z.posSlotDecoders = make([]*rangeBitTreeDecoder, kNumLenToPosStates)
	for i := 0; i < kNumLenToPosStates; i++ {
		z.posSlotDecoders[i] = newRangeBitTreeDecoder(kNumPosSlotBits)
	}

	// z.posAlignDecoder
	z.posAlignDecoder = newRangeBitTreeDecoder(kNumAlignBits)

	// start decoding data
	if err = z.doDecode(); err != nil {
		return
	}
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

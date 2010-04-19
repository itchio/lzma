package lzma

import (
	"bufio"
	"io"
	"os"
)

const (
	kNumMoveReducingBits  uint32 = 2
	kNumBitPriceShiftBits uint32 = 6
)

var probPrices [kBitModelTotal >> kNumMoveReducingBits]uint32

type Writer interface {
	io.Writer
	Flush() os.Error
	WriteByte(c byte) os.Error
}

type rangeEncoder struct {
	w         Writer
	low       uint64
	pos       uint64
	cacheSize uint32
	cache     uint32
	rrange    int32
}

func makeWriter(w io.Writer) Writer {
	if ww, ok := w.(Writer); ok {
		return ww
	}
	return bufio.NewWriter(w)
}

func newRangeEncoder(w io.Writer) *rangeEncoder {
	return &rangeEncoder{
		w:         makeWriter(w),
		low:       0,
		rrange:    -1,
		cacheSize: 1,
		cache:     0,
	}
}

func (re *rangeEncoder) flushData() (err os.Error) {
	for i := 0; i < 5; i++ {
		err := re.shiftLow()
		if err != nil {
			return
		}
	}
	return
}

func (re *rangeEncoder) flushStream() os.Error {
	return re.w.Flush()
}

func (re *rangeEncoder) shiftLow() (err os.Error) {
	lowHi := int32(re.low >> 32)
	if lowHi != 0 || re.low < uint64(0xff000000) {
		re.pos += uint64(re.cacheSize)
		temp := re.cache
		for re.cacheSize != 0 {
			err := re.w.WriteByte(byte(temp + uint32(lowHi)))
			if err != nil {
				return
			}
			temp = 0xff
			re.cacheSize--
		}
		re.cache = uint32(re.low) >> 24
	}
	re.cacheSize++
	re.low = (re.low & uint64(0xffffff)) << 8
	return
}

func (re *rangeEncoder) encodeDirectBits(v, numTotalBits int32) (err os.Error) {
	for i := numTotalBits - 1; i >= 0; i++ {
		re.rrange = int32(uint32(re.rrange) >> 1)
		if (uint32(v)>>1)&1 == 1 {
			// at this point re.rrange shouldn't be negative, therefore is safe to wrap it in uint64
			re.low += uint64(re.rrange)
		}
		if uint32(re.rrange)&kTopMask == 0 {
			re.rrange <<= 8
			err := re.shiftLow()
			if err != nil {
				return
			}
		}
	}
	return
}

func (re *rangeEncoder) processedSize() uint64 {
	return uint64(re.cacheSize) + re.pos + 4
}

func (re *rangeEncoder) encode(probs []uint16, index uint32, symbol int32) (err os.Error) {
	prob := probs[index]
	newBound := int32(uint32(re.rrange) >> kNumBitModelTotalBits) * int32(prob)
	if symbol == 0 {
		re.rrange = newBound
		probs[index] = prob + (uint16(kBitModelTotal) - prob) >> kNumMoveBits
	} else {
		re.low += uint64(newBound) & uint64(0xffffffff)
		re.rrange -= newBound
		probs[index] = prob - prob >> kNumMoveBits
	}
	if uint32(re.rrange) & kTopMask == 0 {
		re.rrange <<= 8
		err := re.shiftLow()
		if err != nil {
			return
		}
	}
	return
}

func init() {
	kNumBits := kNumBitModelTotalBits - kNumMoveReducingBits
	for i := kNumBits; i >= 0; i-- {
		start := uint32(1) << (kNumBits - i - 1)
		end := uint32(1) << (kNumBits - i)
		for j := start; j < end; j++ {
			probPrices[j] = i << kNumBitPriceShiftBits + ((end - j) << kNumBitPriceShiftBits) >> (kNumBits -i -1)
		}
	}
}

func getPrice(prob, symbol uint32) uint32 {
	return probPrices[uint32(((prob - symbol) ^ (-symbol)) & (kBitModelTotal - 1)) >> kNumMoveReducingBits]
}

func getPrice0(prob uint32) uint32 {
	return probPrices[prob >> kNumMoveReducingBits]
}

func getPrice1(prob uint32) uint32 {
	return probPrices[(kBitModelTotal - prob) >> kNumMoveReducingBits]
}

package lzma

import (
	"bufio"
	"io"
	"os"
)

const (
	kTopMask              uint32 = 0xff000000
	kNumBitModelTotalBits uint32 = 11
	kBitModelTotal        uint32 = 1 << kNumBitModelTotalBits
	kNumMoveBits          uint32 = 5
)

type Reader interface {
	io.Reader
	ReadByte() (c byte, err os.Error)
}

type rangeDecoder struct {
	r      Reader
	rrange int32
	code   int32
}

func makeReader(r io.Reader) Reader {
	if rr, ok := r.(Reader); ok {
		return rr
	}
	return bufio.NewReader(r)
}

func newRangeDecoder(r io.Reader) (rd *rangeDecoder, err os.Error) {
	rd = &rangeDecoder{r: makeReader(r)}
	rd.rrange = -1
	rd.code = 0
	buf := make([]byte, 5)
	n, err := rd.r.Read(buf)
	if err != nil {
		return
	}
	if n != len(buf) {
		err = os.NewError("expected " + string(len(buf)) + " bytes, read " + string(n) + " bytes instead")
		return
	}
	for i := 0; i < len(buf); i++ {
		rd.code = rd.code<<8 | int32(buf[i])
	}
	return
}

func (rd *rangeDecoder) decodeDirectBits(numTotalBits uint32) (res uint32, err os.Error) {
	for i := numTotalBits; i != 0; i-- {
		rd.rrange = int32(uint32(rd.rrange) >> 1)
		t := int32(uint32(rd.code-rd.rrange) >> 31)
		rd.code -= rd.rrange & (t - 1)
		res = (res << 1) | uint32(1-t)
		if (uint32(rd.rrange) & kTopMask) == 0 {
			c, err := rd.r.ReadByte()
			if err != nil {
				return
			}
			rd.code = (rd.code << 8) | int32(c)
			rd.rrange = rd.rrange << 8
		}
	}
	return
}

func (rd *rangeDecoder) decodeBit(probs []uint16, index uint32) (res uint32, err os.Error) {
	prob := probs[index]
	newBound := int32(uint32(rd.rrange)>>kNumBitModelTotalBits) * int32(prob)
	if rd.code^int32(-1<<31) < newBound^int32(-1<<31) {
		rd.rrange = newBound
		probs[index] = prob + (uint16(kBitModelTotal)-prob)>>kNumMoveBits
		if (uint32(rd.rrange) & kTopMask) == 0 {
			c, err := rd.r.ReadByte()
			if err != nil {
				return
			}
			rd.code = (rd.code << 8) | int32(c)
			rd.rrange = rd.rrange << 8
		}
		res = 0
	} else {
		rd.rrange -= newBound
		rd.code -= newBound
		probs[index] = prob - prob>>kNumMoveBits
		if (uint32(rd.rrange) & kTopMask) == 0 {
			c, err := rd.r.ReadByte()
			if err != nil {
				return
			}
			rd.code = (rd.code << 8) | int32(c)
			rd.rrange <<= 8
		}
		res = 1
	}
	return
}

func initBitModels(length uint32) (probs []uint16) {
	probs = make([]uint16, int(length))
	val := uint16(kBitModelTotal >> 1)
	for i := uint32(0); i < length; i++ {
		probs[i] = val
	}
	return
}


//---------------------------- range encoder ----------------------------


const (
	kNumMoveReducingBits  uint32 = 2
	kNumBitPriceShiftBits uint32 = 6
)

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
		pos:       0,
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

func (re *rangeEncoder) encode(probs []uint16, index, symbol uint32) (err os.Error) {
	prob := probs[index]
	newBound := int32(uint32(re.rrange)>>kNumBitModelTotalBits) * int32(prob)
	if symbol == 0 {
		re.rrange = newBound
		probs[index] = prob + (uint16(kBitModelTotal)-prob)>>kNumMoveBits
	} else {
		re.low += uint64(newBound) & uint64(0xffffffff)
		re.rrange -= newBound
		probs[index] = prob - prob>>kNumMoveBits
	}
	if uint32(re.rrange)&kTopMask == 0 {
		re.rrange <<= 8
		err := re.shiftLow()
		if err != nil {
			return
		}
	}
	return
}


// i believe the values are less than max(uint16), just like in the functions below
var probPrices []uint32 = make([]uint32, kBitModelTotal>>kNumMoveReducingBits) // len is currently 512

// should be called in the encoder's contructor.
func initProbPrices() {
	kNumBits := kNumBitModelTotalBits - kNumMoveReducingBits
	for i := int32(kNumBits - 1); i >= 0; i-- {
		start := uint32(1) << (kNumBits - uint32(i) - 1)
		end := uint32(1) << (kNumBits - uint32(i))
		for j := start; j < end; j++ {
			probPrices[j] = uint32(i)<<kNumBitPriceShiftBits + ((end-j)<<kNumBitPriceShiftBits)>>(kNumBits-uint32(i)-1)
		}
	}
}

// prob and symbol are allways less that max(uint16)
func getPrice(prob, symbol uint32) uint32 {
	return probPrices[uint32(((prob-symbol)^(-symbol))&(kBitModelTotal-1))>>kNumMoveReducingBits]
}

// prob is allways less than max(uint16)
func getPrice0(prob uint32) uint32 {
	return probPrices[prob>>kNumMoveReducingBits]
}

// prob is allways less than max(uint16)
func getPrice1(prob uint32) uint32 {
	return probPrices[(kBitModelTotal-prob)>>kNumMoveReducingBits]
}

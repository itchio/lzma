package lzma

import (
	"bufio"
	"io"
	"os"
)

const (
	kTopValue             = 1 << 24
	kNumBitModelTotalBits = 11
	kBitModelTotal        = 1 << kNumBitModelTotalBits
	kNumMoveBits          = 5
)

type Reader interface {
	io.Reader
	ReadByte() (c byte, err os.Error)
}

type rangeDecoder struct {
	r      Reader
	rrange uint32
	code   uint32
}

func makeReader(r io.Reader) Reader {
	if rr, ok := r.(Reader); ok {
		return rr
	}
	return bufio.NewReader(r)
}

func newRangeDecoder(r io.Reader) *rangeDecoder {
	rd := &rangeDecoder{r: makeReader(r)}
	rd.rrange = 0xFFFFFFFF
	rd.code = 0
	buf := make([]byte, 5)
	n, err := rd.r.Read(buf) // ERR - panic
	if err != nil {
		error(err) // panic, will recover from it in the upper-most level
	}
	if n != len(buf) {
		error(nReadError) // panic, will recover from it in the upper-most level
	}
	for i := 0; i < len(buf); i++ {
		rd.code = rd.code<<8 | uint32(buf[i])
	}
	return rd
}

func (rd *rangeDecoder) decodeDirectBits(numTotalBits uint32) (res uint32) {
	for i := numTotalBits; i != 0; i-- {
		rd.rrange >>= 1
		t := (rd.code - rd.rrange) >> 31
		rd.code -= rd.rrange & (t - 1)
		res = (res << 1) | (1 - t)
		if rd.rrange < kTopValue {
			c, err := rd.r.ReadByte() // ERR - panic
			if err != nil {
				error(err) // panic, will recover from it in the upper-most level
			}
			rd.code = (rd.code << 8) | uint32(c)
			rd.rrange = rd.rrange << 8
		}
	}
	return
}

func (rd *rangeDecoder) decodeBit(probs []uint16, index uint32) (res uint32) {
	prob := probs[index]
	newBound := (rd.rrange >> kNumBitModelTotalBits) * uint32(prob)
	if rd.code < newBound {
		rd.rrange = newBound
		probs[index] = prob + (kBitModelTotal-prob)>>kNumMoveBits
		if rd.rrange < kTopValue {
			c, err := rd.r.ReadByte() // ERR - panic
			if err != nil {
				error(err) // panic, will recover from it in the upper-most level
			}
			rd.code = (rd.code << 8) | uint32(c)
			rd.rrange = rd.rrange << 8
		}
		res = 0
	} else {
		rd.rrange -= newBound
		rd.code -= newBound
		probs[index] = prob - prob>>kNumMoveBits
		if rd.rrange < kTopValue {
			c, err := rd.r.ReadByte() // ERR - panic
			if err != nil {
				error(err) // panic, will recover from it in the upper-most level
			}
			rd.code = (rd.code << 8) | uint32(c)
			rd.rrange <<= 8
		}
		res = 1
	}
	return
}

func initBitModels(length uint32) (probs []uint16) {
	probs = make([]uint16, length)
	val := uint16(kBitModelTotal) >> 1
	for i := uint32(0); i < length; i++ {
		probs[i] = val
	}
	return
}


const (
	kNumMoveReducingBits  = 2
	kNumBitPriceShiftBits = 6
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
	rrange    uint32
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
		pos:       0,
		cacheSize: 1,
		cache:     0,
		rrange:    0xFFFFFFFF,
	}
}

func (re *rangeEncoder) flush() {
	for i := 0; i < 5; i++ {
		re.shiftLow()
	}
	err := re.w.Flush() // ERR - panic
	if err != nil {
		error(err) // panic, will recover from it in the upper-most level
	}
}

func (re *rangeEncoder) shiftLow() {
	lowHi := uint32(re.low >> 32)
	if lowHi != 0 || re.low < uint64(0xFF000000) {
		re.pos += uint64(re.cacheSize)
		temp := re.cache
		dwtemp := uint32(1) // do-while tmp var, execute the loop at least once
		for ; dwtemp != 0; dwtemp = re.cacheSize {
			err := re.w.WriteByte(byte(temp + lowHi)) // ERR - panic
			if err != nil {
				error(err) // panic, will recover from it in the upper-most level
			}
			temp = 0xFF
			re.cacheSize--
		}
		re.cache = uint32(re.low) >> 24
	}
	re.cacheSize++
	re.low = uint64(uint32(re.low) << 8)
}

func (re *rangeEncoder) encodeDirectBits(v, numTotalBits uint32) {
	for i := numTotalBits - 1; int32(i) >= 0; i-- {
		re.rrange >>= 1
		if (v>>i)&1 == 1 {
			re.low += uint64(re.rrange)
		}
		if re.rrange < kTopValue {
			re.rrange <<= 8
			re.shiftLow()
		}
	}
}

func (re *rangeEncoder) processedSize() uint64 {
	return uint64(re.cacheSize) + re.pos + 4
}

func (re *rangeEncoder) encode(probs []uint16, index, symbol uint32) {
	prob := probs[index]
	newBound := (re.rrange >> kNumBitModelTotalBits) * uint32(prob)
	if symbol == 0 {
		re.rrange = newBound
		probs[index] = prob + (kBitModelTotal-prob)>>kNumMoveBits
	} else {
		re.low += uint64(newBound) & uint64(0xFFFFFFFF)
		re.rrange -= newBound
		probs[index] = prob - prob>>kNumMoveBits
	}
	if re.rrange < kTopValue {
		re.rrange <<= 8
		re.shiftLow()
	}
}


var probPrices []uint32 = make([]uint32, kBitModelTotal>>kNumMoveReducingBits) // len(probPrices) = 512

// should be called in the encoder's contructor.
func initProbPrices() {
	kNumBits := uint32(kNumBitModelTotalBits - kNumMoveReducingBits)
	for i := kNumBits - 1; int32(i) >= 0; i-- {
		start := uint32(1) << (kNumBits - i - 1)
		end := uint32(1) << (kNumBits - i)
		for j := start; j < end; j++ {
			probPrices[j] = i<<kNumBitPriceShiftBits + ((end-j)<<kNumBitPriceShiftBits)>>(kNumBits-i-1)
		}
	}
}

// prob and symbol fit in uint16s. prob is always some element of a []uin16. symbol is usualy an uint32.
// Therefore, in order to save a lot of type conversions, prob must be uint16 and symbol uint32
func getPrice(prob uint16, symbol uint32) uint32 {
	return probPrices[(((uint32(prob)-symbol)^(-symbol))&(uint32(kBitModelTotal)-1))>>kNumMoveReducingBits]
}

func getPrice0(prob uint16) uint32 {
	return probPrices[prob>>kNumMoveReducingBits]
}

func getPrice1(prob uint16) uint32 {
	return probPrices[(kBitModelTotal-prob)>>kNumMoveReducingBits]
}

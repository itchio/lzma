package lzma

import (
	"bufio"
	"fmt"
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
	lowHi := uint32(re.low >> 32)

	fmt.Printf("[0] re.shiftLow(): re.low = %d, re.pos = %d, re.cacheSize = %d, re.cache = %d, re.rrange = %d, lowHi = %d\n",
		re.low, re.pos, re.cacheSize, re.cache, re.rrange, lowHi)

	if re.low == 4449935872 {
		panic("re.low is 4449935872")
	}

	if lowHi != 0 || re.low < uint64(0xff000000) {
		re.pos += uint64(re.cacheSize)
		temp := re.cache
		dwtemp := uint32(1) // do-while tmp var, execute the loop at least once
		for ; dwtemp != 0; dwtemp = re.cacheSize {
			err := re.w.WriteByte(byte(temp + uint32(lowHi)))
			if err != nil {
				return
			}

			fmt.Printf("[1] re.shiftLow(): re.low = %d, re.pos = %d, re.cacheSize = %d, re.cache = %d, re.rrange = %d, lowHi = %d, temp = %d, byte = %d\n",
				re.low, re.pos, re.cacheSize, re.cache, re.rrange, lowHi, temp, int8(byte(temp+uint32(lowHi))))

			temp = 0xff
			re.cacheSize--
		}
		re.cache = uint32(re.low) >> 24
	}
	re.cacheSize++
	//re.low = (re.low & uint64(0xffffff)) << 8
	re.low = uint64(uint32(re.low) << 8)

	fmt.Printf("[2] re.shiftLow(): re.low = %d, re.pos = %d, re.cacheSize = %d, re.cache = %d, re.rrange = %d, lowHi = %d\n",
		re.low, re.pos, re.cacheSize, re.cache, re.rrange, lowHi)

	return
}

func (re *rangeEncoder) encodeDirectBits(v, numTotalBits int32) (err os.Error) {
	for i := numTotalBits - 1; i >= 0; i-- {
		re.rrange = int32(uint32(re.rrange) >> 1)
		if (uint32(v)>>uint32(i))&1 == 1 {
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

	fmt.Printf("[0] re.encode(): re.rrange = %d, re.low = %d, prob = %d, index = %d, symbol = %d, newBound = %d\n",
		re.rrange, re.low, prob, index, symbol, newBound)
	/*
		if re.rrange == 98647040 && prob == 1206 {
			panic("re.rrange == 98647040 && prob == 1206")
		}
	*/
	if symbol == 0 {
		re.rrange = newBound
		probs[index] = prob + (uint16(kBitModelTotal)-prob)>>kNumMoveBits
	} else {
		re.low += uint64(newBound) & uint64(0xffffffff)
		re.rrange -= newBound
		probs[index] = prob - prob>>kNumMoveBits
	}

	fmt.Printf("[1] re.encode(): re.rrange = %d, re.low = %d, prob = %d, index = %d, symbol = %d, newBound = %d, probs[index] = %d\n",
		re.rrange, re.low, prob, index, symbol, newBound, probs[index])

	if uint32(re.rrange)&kTopMask == 0 {
		re.rrange <<= 8

		fmt.Printf("[2] re.encode(): re.rrange = %d, re.low = %d, prob = %d, index = %d, symbol = %d, newBound = %d\n",
			re.rrange, re.low, prob, index, symbol, newBound)

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
	for i := int32(kNumBits) - 1; i >= 0; i-- { // i must remain signed
		start := uint32(1) << (kNumBits - uint32(i) - 1)
		end := uint32(1) << (kNumBits - uint32(i))
		for j := start; j < end; j++ {
			/*
				fmt.Printf("[0] initPribPrices(): len(probPrices) = %d, i = %d, j = %d, start = %d, end = %d, kNumBits = %d\n",
					len(probPrices), i, j, start, end, kNumBits)
			*/
			probPrices[j] = uint32(i)<<kNumBitPriceShiftBits + ((end-j)<<kNumBitPriceShiftBits)>>(kNumBits-uint32(i)-1)
			/*
				fmt.Printf("[1] initProbPrices(): i = %d, j = %d, kNumBits = %d, start = %d, end = %d, probPrices[j] = %d\n",
					i, j, kNumBits, start, end, probPrices[j])
			*/
		}
	}
}

// prob and symbol are allways less that max(uint16)
func getPrice(prob, symbol uint32) uint32 {
	res := probPrices[uint32(((prob-symbol)^(-symbol))&(kBitModelTotal-1))>>kNumMoveReducingBits]
	/*
		fmt.Printf("[0] getPrice(): prob = %d, symbol = %d, index = %d, res = %d, kBitModelTotal = %d, kNumMoveReducingBits = %d\n",
			prob, symbol, uint32(((prob-symbol)^(-symbol))&(kBitModelTotal-1))>>kNumMoveReducingBits, res, kBitModelTotal, kNumMoveReducingBits)
	*/
	return res
}

// prob is allways less than max(uint16)
func getPrice0(prob uint32) uint32 {
	res := probPrices[prob>>kNumMoveReducingBits]
	/*
		fmt.Printf("[0] getPrice0(): prob = %d, index = %d, res = %d, kNumMoveReducingBits = %d\n", prob, prob>>kNumMoveReducingBits, res, kNumMoveReducingBits)
	*/
	return res
}

// prob is allways less than max(uint16)
func getPrice1(prob uint32) uint32 {
	res := probPrices[(kBitModelTotal-prob)>>kNumMoveReducingBits]
	/*
		fmt.Printf("[0] getPrice1(): prob = %d, index = %d, res = %d, kNumMoveReducingBits = %d, kBitModelTotal = %d\n",
			prob, (kBitModelTotal-prob)>>kNumMoveReducingBits, res, kNumMoveReducingBits, kBitModelTotal)
	*/
	return res
}

func printProbPrices_temp_F() {

	fmt.Printf("[0] printProbPrices_temp_F(): len(pribPrices) = %d\n", len(probPrices))
	for i := 0; i < len(probPrices); i++ {
		fmt.Printf("[1] printProbPrices_temp_F(): i = %d, pribPrices[i] = %d\n", i, probPrices[i])
	}

}

// prob is allways less than max(uint16)
func getPrice0_temp_F(prob uint32) uint32 {
	res := probPrices[prob>>kNumMoveReducingBits]

	fmt.Printf("[0] getPrice0_temp_F(): prob = %d, index = %d, res = %d, kNumMoveReducingBits = %d\n", prob, prob>>kNumMoveReducingBits, res, kNumMoveReducingBits)

	return res
}

// prob is allways less than max(uint16)
func getPrice1_temp_F(prob uint32) uint32 {
	res := probPrices[(kBitModelTotal-prob)>>kNumMoveReducingBits]

	fmt.Printf("[0] getPrice1_temp_F(): prob = %d, index = %d, res = %d, kNumMoveReducingBits = %d, kBitModelTotal = %d\n",
		prob, (kBitModelTotal-prob)>>kNumMoveReducingBits, res, kNumMoveReducingBits, kBitModelTotal)

	return res
}

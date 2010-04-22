package lzma

import "os"

type lenCoder struct {
	choice    []uint16
	lowCoder  []*rangeBitTreeCoder
	midCoder  []*rangeBitTreeCoder
	highCoder *rangeBitTreeCoder
}

func newLenCoder(numPosStates uint32) *lenCoder {
	lc := &lenCoder{
		choice:    initBitModels(2),
		lowCoder:  make([]*rangeBitTreeCoder, kNumPosStatesMax),
		midCoder:  make([]*rangeBitTreeCoder, kNumPosStatesMax),
		highCoder: newRangeBitTreeCoder(kNumHighLenBits),
	}
	for i := uint32(0); i < numPosStates; i++ {
		lc.lowCoder[i] = newRangeBitTreeCoder(kNumLowLenBits)
		lc.midCoder[i] = newRangeBitTreeCoder(kNumMidLenBits)
	}
	return lc
}

// ---------------- decode --------------------

func (lc *lenCoder) decode(rd *rangeDecoder, posState uint32) (res uint32, err os.Error) {
	i, err := rd.decodeBit(lc.choice, 0)
	if err != nil {
		return
	}
	if i == 0 {
		res, err = lc.lowCoder[posState].decode(rd)
		return
	}
	res = kNumLowLenSymbols
	j, err := rd.decodeBit(lc.choice, 1)
	if err != nil {
		return
	}
	if j == 0 {
		k, err := lc.midCoder[posState].decode(rd)
		if err != nil {
			return
		}
		res += k
		return
	} else {
		l, err := lc.highCoder.decode(rd)
		if err != nil {
			return
		}
		res = res + kNumMidLenSymbols + l
		return
	}
	return
}

// ---------------- encode --------------------

func (lc *lenCoder) encode(re *rangeEncoder, symbol, posState uint32) (err os.Error) {
	if symbol < kNumLowLenSymbols {
		err = re.encode(lc.choice, 0, 0)
		if err != nil {
			return
		}
		err = lc.lowCoder[posState].encode(re, symbol)
		if err != nil {
			return
		}
	} else {
		symbol -= kNumLowLenSymbols
		err = re.encode(lc.choice, 0, 1)
		if err != nil {
			return
		}
		if symbol < kNumMidLenSymbols {
			err = re.encode(lc.choice, 1, 0)
			if err != nil {
				return
			}
			err = lc.midCoder[posState].encode(re, symbol)
			if err != nil {
				return
			}
		} else {
			err = re.encode(lc.choice, 1, 1)
			if err != nil {
				return
			}
			err = lc.highCoder.encode(re, symbol-kNumMidLenSymbols)
			if err != nil {
				return
			}
		}
	}
	return
}

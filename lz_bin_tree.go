package lzma

import (
	"io"
	"os"
)

const (
	kHash2Size          int32 = 1 << 10
	kHash3Size          int32 = 1 << 16
	kBT2HashSize        int32 = 1 << 16
	kStartMaxLen        int32 = 1
	kHash3Offset        int32 = kHash2Size
	kEmptyHashValue     int32 = 0
	kMaxValForNormalize int32 = (1 << 30) - 1
)

type lzBinTree struct {
	iw                  *lzInWindow //
	son                 []int32     //
	hash                []int32     //
	cyclicBufPos        int32       //
	cyclicBufSize       int32       //
	matchMaxLen         int32       //
	cutValue            int32       //
	hashMask            int32       //
	hashSizeSum         int32       //
	kNumHashDirectBytes int32       //
	kMinMatchCheck      int32       //
	kFixHashSize        int32       //
	hashArray           bool        //
}

func newLzBinTree(r io.Reader, historySize, keepAddBufBefore, matchMaxLen, keepAddBufAfter, numHashBytes int32) (bt *lzBinTree, err os.Error) {
	bt = &lzBinTree{
		son:           make([]int32, (historySize+1)*2),
		cyclicBufPos:  0,
		cyclicBufSize: historySize + 1, // default was 0
		matchMaxLen:   matchMaxLen,
		cutValue:      16 + (matchMaxLen >> 1), // default was 0xff
		//hashSizeSum:			// default was 0
		//kNumHashDirectBytes:		// default was 0
		//kMinMatchCheck:		// default was 4
		//kFixHashSize:			// defautt was kHash2Size + kHash3Size
	}

	winSizeReserv := (historySize+keepAddBufBefore+matchMaxLen+keepAddBufAfter)/2 + 256
	bt.iw, err = newLzInWindow(r, historySize+keepAddBufBefore, matchMaxLen+keepAddBufAfter, winSizeReserv)
	if err != nil {
		return
	}

	if numHashBytes > 2 {
		bt.hashArray = true
		bt.kNumHashDirectBytes = 0
		bt.kMinMatchCheck = 4
		bt.kFixHashSize = kHash2Size + kHash3Size
	} else {
		bt.hashArray = false
		bt.kNumHashDirectBytes = 2
		bt.kMinMatchCheck = 3
		bt.kFixHashSize = 0
	}

	hs := kBT2HashSize
	if bt.hashArray == true {
		hs = historySize - 1
		hs |= hs >> 1
		hs |= hs >> 2
		hs |= hs >> 4
		hs |= hs >> 8
		hs >>= 1
		hs |= 0xffff
		if hs > 1<<24 {
			hs >>= 1
		}
		bt.hashMask = hs
		hs++
		hs += bt.kFixHashSize
	}
	bt.hashSizeSum = hs
	bt.hash = make([]int32, bt.hashSizeSum)
	for i := int32(0); i < bt.hashSizeSum; i++ {
		bt.hash[i] = kEmptyHashValue
	}

	bt.iw.reduceOffsets(-1)
	return
}

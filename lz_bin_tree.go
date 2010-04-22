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
	iw                   *lzInWindow
	son                  []int32
	hash                 []int32
	cyclicBufPos         int32
	cyclicBufSize        int32
	matchMaxLen          int32
	cutValue             int32
	hashMask             int32
	hashSizeSum          int32
	kvNumHashDirectBytes int32
	kvMinMatchCheck      int32
	kvFixHashSize        int32
	hashArray            bool
}

func newLzBinTree(r io.Reader, historySize, keepAddBufBefore, matchMaxLen, keepAddBufAfter, numHashBytes int32) (bt *lzBinTree, err os.Error) {
	bt = &lzBinTree{
		son:           make([]int32, (historySize+1)*2),
		cyclicBufPos:  0,
		cyclicBufSize: historySize + 1,
		matchMaxLen:   matchMaxLen,
		cutValue:      16 + (matchMaxLen >> 1),
	}

	winSizeReserv := (historySize+keepAddBufBefore+matchMaxLen+keepAddBufAfter)/2 + 256
	bt.iw, err = newLzInWindow(r, historySize+keepAddBufBefore, matchMaxLen+keepAddBufAfter, winSizeReserv)
	if err != nil {
		return
	}

	if numHashBytes > 2 {
		bt.hashArray = true
		bt.kvNumHashDirectBytes = 0
		bt.kvMinMatchCheck = 4
		bt.kvFixHashSize = kHash2Size + kHash3Size
	} else {
		bt.hashArray = false
		bt.kvNumHashDirectBytes = 2
		bt.kvMinMatchCheck = 3
		bt.kvFixHashSize = 0
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
		hs += bt.kvFixHashSize
	}
	bt.hashSizeSum = hs
	bt.hash = make([]int32, bt.hashSizeSum)
	for i := int32(0); i < bt.hashSizeSum; i++ {
		bt.hash[i] = kEmptyHashValue
	}

	bt.iw.reduceOffsets(-1)
	return
}

func normalizeLinks(items []int32, numItems, subValue int32) {
	for i := int32(0); i < numItems; i++ {
		value := items[i]
		if value <= subValue {
			value = kEmptyHashValue
		} else {
			value -= subValue
		}
		items[i] = value
	}
}

func (bt *lzBinTree) normalize() {
	subValue := bt.iw.pos - bt.cyclicBufSize
	normalizeLinks(bt.son, bt.cyclicBufSize*2, subValue)
	normalizeLinks(bt.hash, bt.hashSizeSum, subValue)
	bt.iw.reduceOffsets(subValue)
}

func (bt *lzBinTree) movePos() (err os.Error) {
	bt.cyclicBufPos++
	if bt.cyclicBufPos >= bt.cyclicBufSize {
		bt.cyclicBufPos = 0
	}
	err = bt.iw.movePos()
	if err != nil {
		return
	}
	if bt.iw.pos == kMaxValForNormalize {
		bt.normalize()
	}
	return
}

func (bt *lzBinTree) getMatches(distances []int32) (res int32, err os.Error) {
	var lenLimit int32
	if bt.iw.pos+bt.matchMaxLen <= bt.iw.streamPos {
		lenLimit = bt.matchMaxLen
	} else {
		lenLimit = bt.iw.streamPos - bt.iw.pos
		if lenLimit < bt.kvMinMatchCheck {
			err = bt.movePos()
			if err != nil {
				return
			}
			return 0, nil
		}
	}

	offset := int32(0)
	matchMinPos := int32(0)
	if bt.iw.pos > bt.cyclicBufSize {
		matchMinPos = bt.iw.pos - bt.cyclicBufSize
	}
	cur := bt.iw.bufOffset + bt.iw.pos
	maxLen := kStartMaxLen
	var hashValue int32
	hash2Value := int32(0)
	hash3Value := int32(0)

	if bt.hashArray == true {
		tmp := crcTable[bt.iw.buf[cur]&0xFF] ^ int32(int8(bt.iw.buf[cur+1]&0xFF))
		hash2Value = tmp & (kHash2Size - 1)
		tmp ^= int32(int8(bt.iw.buf[cur+2]&0xFF)) << 8
		hash3Value = tmp & (kHash3Size - 1)
		hashValue = (tmp ^ crcTable[bt.iw.buf[cur+3]&0xFF]<<5) & bt.hashMask
	} else {
		hashValue = int32(int8(bt.iw.buf[cur]&0xFF)) ^ int32(int8(bt.iw.buf[cur+1]&0xFF))<<8
	}

	curMatch := bt.hash[bt.kvFixHashSize+hashValue]
	if bt.hashArray == true {
		curMatch2 := bt.hash[hash2Value]
		curMatch3 := bt.hash[kHash3Offset+hash3Value]
		bt.hash[hash2Value] = bt.iw.pos
		bt.hash[kHash3Offset+hash3Value] = bt.iw.pos
		if curMatch2 > matchMinPos {
			if bt.iw.buf[bt.iw.bufOffset+curMatch2] == bt.iw.buf[cur] {
				maxLen = 2
				distances[offset] = maxLen
				offset++
				distances[offset] = bt.iw.pos - curMatch2 - 1
				offset++
			}
		}
		if curMatch3 > matchMinPos {
			if bt.iw.buf[bt.iw.bufOffset+curMatch3] == bt.iw.buf[cur] {
				if curMatch3 == curMatch2 {
					offset -= 2
				}
				maxLen = 3
				distances[offset] = maxLen
				offset++
				distances[offset] = bt.iw.pos - curMatch3 - 1
				offset++
				curMatch2 = curMatch3
			}
		}
		if offset != 0 && curMatch2 == curMatch {
			offset -= 2
			maxLen = kStartMaxLen
		}
	}

	bt.hash[bt.kvFixHashSize+hashValue] = bt.iw.pos

	if bt.kvNumHashDirectBytes != 0 {
		if curMatch > matchMinPos {
			if bt.iw.buf[bt.iw.bufOffset+curMatch+bt.kvNumHashDirectBytes] != bt.iw.buf[cur+bt.kvNumHashDirectBytes] {
				maxLen = bt.kvNumHashDirectBytes
				distances[offset] = maxLen
				offset++
				distances[offset] = bt.iw.pos - curMatch - 1
				offset++
			}
		}
	}

	ptr0 := bt.cyclicBufPos<<1 + 1
	ptr1 := bt.cyclicBufPos << 1
	len0 := bt.kvNumHashDirectBytes
	len1 := bt.kvNumHashDirectBytes
	count := bt.cutValue

	for {
		if curMatch <= matchMinPos || count == 0 {
			bt.son[ptr1] = kEmptyHashValue
			bt.son[ptr0] = kEmptyHashValue
			break
		}
		count--

		delta := bt.iw.pos - curMatch
		var cyclicPos int32
		if delta <= bt.cyclicBufPos {
			cyclicPos = (bt.cyclicBufPos - delta) << 1
		} else {
			cyclicPos = (bt.cyclicBufPos - delta + bt.cyclicBufSize) << 1
		}
		pby1 := bt.iw.bufOffset + curMatch
		var length int32
		if len0 <= len1 {
			length = len0
		} else {
			length = len1
		}

		if bt.iw.buf[pby1+length] == bt.iw.buf[cur+length] {
			for length++; length != lenLimit; length++ {
				if bt.iw.buf[pby1+length] != bt.iw.buf[cur+length] {
					break
				}
			}
			if maxLen < length {
				maxLen = length
				distances[offset] = maxLen
				offset++
				distances[offset] = delta - 1
				offset++
				if length == lenLimit {
					bt.son[ptr1] = bt.son[cyclicPos]
					bt.son[ptr0] = bt.son[cyclicPos+1]
					break
				}
			}
		}

		if bt.iw.buf[pby1+length]&0xFF < bt.iw.buf[cur+length]&0xFF {
			bt.son[ptr1] = curMatch
			ptr1 = cyclicPos + 1
			curMatch = bt.son[ptr1]
			len1 = length
		} else {
			bt.son[ptr0] = curMatch
			ptr0 = cyclicPos
			curMatch = bt.son[ptr0]
			len0 = length
		}
	}

	err = bt.movePos()
	return offset, err
}

func (bt *lzBinTree) skip(num int32) (err os.Error) {
	for i := int32(0); i < num; i++ {
		var lenLimit int32
		if bt.iw.pos+bt.matchMaxLen <= bt.iw.streamPos {
			lenLimit = bt.matchMaxLen
		} else {
			lenLimit = bt.iw.streamPos - bt.iw.pos
			if lenLimit < bt.kvMinMatchCheck {
				err = bt.movePos()
				if err != nil {
					return
				}
				continue
			}
		}

		matchMinPos := int32(0)
		if bt.iw.pos > bt.cyclicBufSize {
			matchMinPos = bt.iw.pos - bt.cyclicBufSize
		}
		cur := bt.iw.bufOffset + bt.iw.pos
		var hashValue int32
		if bt.hashArray == true {
			tmp := crcTable[bt.iw.buf[cur]&0xFF] ^ int32(int8(bt.iw.buf[cur+1]&0xFF))
			hash2Value := tmp & (kHash2Size - 1)
			bt.hash[hash2Value] = bt.iw.pos
			tmp ^= int32(int8(bt.iw.buf[cur+2]&0xFF)) << 8
			hash3Value := tmp & (kHash3Size - 1)
			bt.hash[kHash3Offset+hash3Value] = bt.iw.pos
			hashValue = (tmp ^ crcTable[bt.iw.buf[cur+3]&0xFF]<<5) & bt.hashMask
		} else {
			hashValue = int32(int8(bt.iw.buf[cur]&0xFF)) ^ int32(int8(bt.iw.buf[cur+1]&0xFF))<<8
		}

		curMatch := bt.hash[bt.kvFixHashSize+hashValue]
		bt.hash[bt.kvFixHashSize+hashValue] = bt.iw.pos
		ptr0 := bt.cyclicBufPos<<1 + 1
		ptr1 := bt.cyclicBufPos << 1
		len0 := bt.kvNumHashDirectBytes
		len1 := bt.kvNumHashDirectBytes
		count := bt.cutValue
		for {
			if curMatch <= matchMinPos || count == 0 {
				bt.son[ptr1] = kEmptyHashValue
				bt.son[ptr0] = kEmptyHashValue
				break
			}
			count--

			delta := bt.iw.pos - curMatch
			var cyclicPos int32
			if delta <= bt.cyclicBufPos {
				cyclicPos = (bt.cyclicBufPos - delta) << 1
			} else {
				cyclicPos = (bt.cyclicBufPos - delta + bt.cyclicBufSize) << 1
			}
			pby1 := bt.iw.bufOffset + curMatch
			var length int32
			if len0 <= len1 {
				length = len0
			} else {
				length = len1
			}
			if bt.iw.buf[pby1+length] == bt.iw.buf[cur+length] {
				for length++; length != lenLimit; length++ {
					if bt.iw.buf[pby1+length] != bt.iw.buf[cur+length] {
						break
					}
				}
				if length == lenLimit {
					bt.son[ptr1] = bt.son[cyclicPos]
					bt.son[ptr0] = bt.son[cyclicPos+1]
					break
				}
			}

			if bt.iw.buf[pby1+length]&0xFF < bt.iw.buf[cur+length]&0xFF {
				bt.son[ptr1] = curMatch
				ptr1 = cyclicPos + 1
				curMatch = bt.son[ptr1]
				len1 = length
			} else {
				bt.son[ptr0] = curMatch
				ptr0 = cyclicPos
				curMatch = bt.son[ptr0]
				len0 = length
			}
		}

		err = bt.movePos()
		if err != nil {
			return
		}
	}
	return
}


var crcTable []int32 = make([]int32, 256)

// should be called in the encoder's contructor
func initCrcTable() {
	for i := int32(0); i < 256; i++ {
		r := i
		for j := 0; j < 8; j++ {
			if r&i != 0 {
				r = int32(uint32(r)>>1 ^ 0xEDB88320)
			} else {
				r = int32(uint32(r) >> 1)
			}
		}
		crcTable[i] = r
	}
}

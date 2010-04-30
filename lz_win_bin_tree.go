package lzma

import (
	"io"
	"os"
)

type lzOutWindow struct {
	w         io.Writer
	buf       []byte
	winSize   uint32
	pos       uint32
	streamPos uint32
}

func newLzOutWindow(w io.Writer, windowSize uint32) *lzOutWindow {
	return &lzOutWindow{
		w:         w,
		buf:       make([]byte, windowSize),
		winSize:   windowSize,
		pos:       0,
		streamPos: 0,
	}
}

func (outWin *lzOutWindow) flush() {
	size := outWin.pos - outWin.streamPos
	if size == 0 {
		return
	}
	n, err := outWin.w.Write(outWin.buf[outWin.streamPos : outWin.streamPos+size]) // ERR - panic
	if err != nil {
		error(err) // panic, will recover from it in the upper-most level
	}
	if uint32(n) != size {
		error(nWriteError) // panic, will recover from it in the upper-most level
	}
	if outWin.pos >= outWin.winSize {
		outWin.pos = 0
	}
	outWin.streamPos = outWin.pos
}

func (outWin *lzOutWindow) copyBlock(distance, length uint32) {
	pos := int32(int32(outWin.pos) - int32(distance) - 1)
	if pos < 0 {
		pos += int32(outWin.winSize)
	}
	for ; length != 0; length-- {
		if pos >= int32(outWin.winSize) {
			pos = 0
		}
		outWin.buf[outWin.pos] = outWin.buf[pos]
		outWin.pos++
		pos++
		if outWin.pos >= outWin.winSize {
			outWin.flush()
		}
	}
}

func (outWin *lzOutWindow) putByte(b byte) {
	outWin.buf[outWin.pos] = b
	outWin.pos++
	if outWin.pos >= outWin.winSize {
		outWin.flush()
	}
}

func (outWin *lzOutWindow) getByte(distance uint32) (b byte) {
	pos := int32(int32(outWin.pos) - int32(distance) - 1)
	if pos < 0 {
		pos += int32(outWin.winSize)
	}
	b = outWin.buf[pos]
	return
}


//----------------------------- lz in window --------------------------------


type lzInWindow struct {
	r              io.Reader
	buf            []byte
	posLimit       uint32
	lastSafePos    uint32
	bufOffset      uint32
	blockSize      uint32
	pos            uint32
	keepSizeBefore uint32
	keepSizeAfter  uint32
	streamPos      uint32
	streamEnd      bool
}

func newLzInWindow(r io.Reader, keepSizeBefore, keepSizeAfter, keepSizeReserv uint32) *lzInWindow {
	blockSize := keepSizeBefore + keepSizeAfter + keepSizeReserv
	iw := &lzInWindow{
		r:              r,
		buf:            make([]byte, blockSize),
		lastSafePos:    blockSize - keepSizeAfter,
		bufOffset:      0,
		blockSize:      blockSize,
		pos:            0,
		keepSizeBefore: keepSizeBefore,
		keepSizeAfter:  keepSizeAfter,
		streamPos:      0,
		streamEnd:      false,
	}
	iw.readBlock()
	return iw
}

func (iw *lzInWindow) moveBlock() {
	offset := iw.bufOffset + iw.pos - iw.keepSizeBefore
	if offset > 0 {
		offset--
	}
	numBytes := iw.bufOffset + iw.streamPos - offset
	for i := uint32(0); i < numBytes; i++ {
		iw.buf[i] = iw.buf[offset+i]
	}
	iw.bufOffset -= offset
}

func (iw *lzInWindow) readBlock() {
	if iw.streamEnd {
		return
	}
	for {
		if iw.blockSize-iw.bufOffset-iw.streamPos == 0 {
			return
		}
		n, err := iw.r.Read(iw.buf[iw.bufOffset+iw.streamPos : iw.blockSize]) // ERR - panic
		if err != nil && err != os.EOF {
			error(err) // panic, will recover from it in upper-most level
		}
		if n == 0 && err == os.EOF {
			iw.posLimit = iw.streamPos
			ptr := iw.bufOffset + iw.posLimit
			if ptr > iw.lastSafePos {
				iw.posLimit = iw.lastSafePos - iw.bufOffset
			}
			iw.streamEnd = true
			return
		}
		iw.streamPos += uint32(n)
		if iw.streamPos >= iw.pos+iw.keepSizeAfter {
			iw.posLimit = iw.streamPos - iw.keepSizeAfter
		}
	}
}

func (iw *lzInWindow) movePos() {
	iw.pos++
	if iw.pos > iw.posLimit {
		ptr := iw.bufOffset + iw.pos
		if ptr > iw.lastSafePos {
			iw.moveBlock()
		}
		iw.readBlock()
	}
}

// signature: c | go | cs (index is a signed int)
func (iw *lzInWindow) getIndexByte(index int32) byte {
	res := iw.buf[int32(iw.bufOffset+iw.pos)+index]

	//fmt.Printf("[0] iw.getIndexByte(): index = %d, len(iw.buf) = %d, iw.bufOffset = %d, iw.pos = %d, theIndex = %d, res = %d\n",
	//	index, len(iw.buf), int32(iw.bufOffset), iw.pos, int32(iw.bufOffset+iw.pos)+index, int8(res))

	return res
}

// only index should be signed
func (iw *lzInWindow) getMatchLen(index int32, distance, limit uint32) (res uint32) {
	if iw.streamEnd == true {
		if uint32(int32(iw.pos)+index)+limit > iw.streamPos {
			limit = iw.streamPos - uint32(int32(iw.pos)+index)
		}
	}
	distance++
	pby := iw.bufOffset + iw.pos + uint32(index)
	for res = uint32(0); res < limit && iw.buf[pby+res] == iw.buf[pby+res-distance]; res++ {
		// empty body
	}
	return
}

// signature: c | go | cs
func (iw *lzInWindow) getNumAvailableBytes() uint32 {
	/*
		if iw.streamPos - iw.pos == 1304583 {
			if navn == false {
				navn = true
			} else {
				panic(1304583)
			}
		}
	*/
	return iw.streamPos - iw.pos
}

// signature: c | go | cs (signed)
func (iw *lzInWindow) reduceOffsets(subValue int32) {
	uSubValue := uint32(subValue)
	iw.bufOffset += uSubValue
	iw.posLimit -= uSubValue
	iw.pos -= uSubValue
	iw.streamPos -= uSubValue
}


//----------------------------------- lz bin tree -----------------------------


const (
	kHash2Size          = 1 << 10
	kHash3Size          = 1 << 16
	kBT2HashSize        = 1 << 16
	kStartMaxLen        = 1
	kHash3Offset        = kHash2Size
	kEmptyHashValue     = 0
	kMaxValForNormalize = (1 << 30) - 1
)

// all should be unsigned
type lzBinTree struct {
	iw                   *lzInWindow
	son                  []uint32
	hash                 []uint32
	cyclicBufPos         uint32
	cyclicBufSize        uint32
	matchMaxLen          uint32
	cutValue             uint32
	hashMask             uint32
	hashSizeSum          uint32
	kvNumHashDirectBytes uint32
	kvMinMatchCheck      uint32
	kvFixHashSize        uint32
	hashArray            bool
}

// signature: c | go | cs (in cs compressionLevel fields are signed, but for no good reason)
func newLzBinTree(r io.Reader, historySize, keepAddBufBefore, matchMaxLen, keepAddBufAfter, numHashBytes uint32) *lzBinTree {

	//fmt.Printf("[0] bt.newLzBinTree(): historySize = %d, keepAddBufBefore= %d, matchMaxLen = %d, keepAddBufAfter = %d\n",
	//	historySize, keepAddBufBefore, matchMaxLen, keepAddBufAfter)

	bt := &lzBinTree{
		son:           make([]uint32, (historySize+1)*2),
		cyclicBufPos:  0,
		cyclicBufSize: historySize + 1,
		matchMaxLen:   matchMaxLen,
		cutValue:      16 + (matchMaxLen >> 1),
	}

	winSizeReserv := (historySize+keepAddBufBefore+matchMaxLen+keepAddBufAfter)/2 + 256
	bt.iw = newLzInWindow(r, historySize+keepAddBufBefore, matchMaxLen+keepAddBufAfter, winSizeReserv)

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

	hs := uint32(kBT2HashSize)
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
	bt.hash = make([]uint32, bt.hashSizeSum)
	for i := uint32(0); i < bt.hashSizeSum; i++ {
		bt.hash[i] = kEmptyHashValue
	}

	bt.iw.reduceOffsets(-1)
	return bt
}

func normalizeLinks(items []uint32, numItems, subValue uint32) {
	for i := uint32(0); i < numItems; i++ {
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
	bt.iw.reduceOffsets(int32(subValue))
}

func (bt *lzBinTree) movePos() {
	bt.cyclicBufPos++
	if bt.cyclicBufPos >= bt.cyclicBufSize {
		bt.cyclicBufPos = 0
	}
	bt.iw.movePos()
	if bt.iw.pos == kMaxValForNormalize {
		bt.normalize()
	}
}

func (bt *lzBinTree) getMatches(distances []uint32) uint32 {
	var lenLimit uint32

	//fmt.Printf("[0] z.mf.getMatches(): bt.iw.pos = %d, bt.matchMaxLen = %d, bt.iw.streamPos = %d\n", bt.iw.pos, bt.matchMaxLen, bt.iw.streamPos)

	if bt.iw.pos+bt.matchMaxLen <= bt.iw.streamPos {
		lenLimit = bt.matchMaxLen
	} else {
		lenLimit = bt.iw.streamPos - bt.iw.pos
		if lenLimit < bt.kvMinMatchCheck {
			bt.movePos()

			//fmt.Printf("[1] z.mf.getMatches(): bt.iw.pos = %d, bt.matchMaxLen = %d, bt.iw.streamPos = %d, lenLimit = %d, bt.kvMinMatchCheck = %d, " +
			//	"result = %d\n", bt.iw.pos, bt.matchMaxLen, bt.iw.streamPos, lenLimit, bt.kvMinMatchCheck, 0)

			return 0
		}
	}

	//fmt.Printf("[2] z.mf.getMatches(): bt.iw.pos = %d, bt.matchMaxLen = %d, bt.iw.streamPos = %d, lenLimit = %d, bt.kvMinMatchCheck = %d\n",
	//	bt.iw.pos, bt.matchMaxLen, bt.iw.streamPos, lenLimit, bt.kvMinMatchCheck)

	offset := uint32(0)
	matchMinPos := uint32(0)
	if bt.iw.pos > bt.cyclicBufSize {
		matchMinPos = bt.iw.pos - bt.cyclicBufSize
	}
	cur := bt.iw.bufOffset + bt.iw.pos
	maxLen := uint32(kStartMaxLen)
	var hashValue uint32
	hash2Value := uint32(0)
	hash3Value := uint32(0)

	if bt.hashArray == true {
		tmp := crcTable[bt.iw.buf[cur]] ^ uint32(bt.iw.buf[cur+1])
		hash2Value = tmp & (kHash2Size - 1)

		//fmt.Printf("[3] z.mf.getMatches(): bt.iw.buf[cur] = %d, bt.iw.buf[cur+1] = %d, crcTable[bt.iw.buf[cur]] = %d, uint32(bt.iw.buf[cur+1]) = %d\n",
		//	bt.iw.buf[cur], bt.iw.buf[cur+1], int32(crcTable[bt.iw.buf[cur]]), uint32(bt.iw.buf[cur+1]))

		//fmt.Printf("[4] z.mf.getMatches(): tmp = %d, hash2Value = %d, hash3Value = %d, cur = %d, kHash2Size = %d\n",
		//	int32(tmp), hash2Value, hash3Value, cur, kHash2Size)

		tmp ^= uint32(bt.iw.buf[cur+2]) << 8
		hash3Value = tmp & (kHash3Size - 1)
		hashValue = (tmp ^ crcTable[bt.iw.buf[cur+3]]<<5) & bt.hashMask

		//fmt.Printf("[5] z.mf.getMatches(): tmp = %d, hashValue = %d, hash2Value = %d, hash3Value = %d,"+
		//	" cur = %d, kHash2Size = %d, kHash3Size = %d, bt.hashMask = %d\n",
		//	int32(tmp), hashValue, hash2Value, hash3Value, cur, kHash2Size, kHash3Size, bt.hashMask)

	} else {
		hashValue = uint32(bt.iw.buf[cur]) ^ uint32(bt.iw.buf[cur+1])<<8
	}

	//fmt.Printf("[6] z.mf.getMatches(): offset = %d, matchMinPos = %d, cur = %d, maxLen = %d, hashValue = %d, hash2Value = %d, hash3Value = %d, bt.hashArray = %t\n",
	//	offset, matchMinPos, cur, maxLen, hashValue, hash2Value, hash3Value, bt.hashArray)

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
		var cyclicPos uint32
		if delta <= bt.cyclicBufPos {
			cyclicPos = (bt.cyclicBufPos - delta) << 1
		} else {
			cyclicPos = (bt.cyclicBufPos - delta + bt.cyclicBufSize) << 1
		}
		pby1 := bt.iw.bufOffset + curMatch
		var length uint32
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

		if bt.iw.buf[pby1+length] < bt.iw.buf[cur+length] {
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

	bt.movePos()

	//fmt.Printf("[7] z.mf.getMatches(): bt.iw.pos = %d, bt.matchMaxLen = %d, bt.iw.streamPos = %d, lenLimit = %d, bt.kvMinMatchCheck = %d, result = %d\n",
	//	bt.iw.pos, bt.matchMaxLen, bt.iw.streamPos, lenLimit, bt.kvMinMatchCheck, offset)

	return offset
}

func (bt *lzBinTree) skip(num uint32) {
	for i := uint32(0); i < num; i++ {
		var lenLimit uint32
		if bt.iw.pos+bt.matchMaxLen <= bt.iw.streamPos {
			lenLimit = bt.matchMaxLen
		} else {
			lenLimit = bt.iw.streamPos - bt.iw.pos
			if lenLimit < bt.kvMinMatchCheck {
				bt.movePos()
				continue
			}
		}

		matchMinPos := uint32(0)
		if bt.iw.pos > bt.cyclicBufSize {
			matchMinPos = bt.iw.pos - bt.cyclicBufSize
		}
		cur := bt.iw.bufOffset + bt.iw.pos
		var hashValue uint32
		if bt.hashArray == true {
			tmp := crcTable[bt.iw.buf[cur]] ^ uint32(bt.iw.buf[cur+1])
			hash2Value := tmp & (kHash2Size - 1)
			bt.hash[hash2Value] = bt.iw.pos
			tmp ^= uint32(bt.iw.buf[cur+2]) << 8
			hash3Value := tmp & (kHash3Size - 1)
			bt.hash[kHash3Offset+hash3Value] = bt.iw.pos
			hashValue = (tmp ^ crcTable[bt.iw.buf[cur+3]]<<5) & bt.hashMask
		} else {
			hashValue = uint32(bt.iw.buf[cur]) ^ uint32(bt.iw.buf[cur+1])<<8
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
			var cyclicPos uint32
			if delta <= bt.cyclicBufPos {
				cyclicPos = (bt.cyclicBufPos - delta) << 1
			} else {
				cyclicPos = (bt.cyclicBufPos - delta + bt.cyclicBufSize) << 1
			}
			pby1 := bt.iw.bufOffset + curMatch
			var length uint32
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

			if bt.iw.buf[pby1+length] < bt.iw.buf[cur+length] {
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
		bt.movePos()
	}
}


var crcTable []uint32 = make([]uint32, 256)

// should be called in the encoder's contructor
func initCrcTable() {
	for i := uint32(0); i < 256; i++ {
		r := i
		for j := 0; j < 8; j++ {
			if r&1 != 0 {
				r = r>>1 ^ 0xEDB88320
			} else {
				r >>= 1
			}
		}
		crcTable[i] = r
	}
}

package lzma

const (
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
	kNumPosStatesBitsEncodingMax    = 4
	kNumPosStatesEncodingMax        = 1 << kNumPosStatesBitsEncodingMax
	kNumLowLenBits                  = 3
	kNumMidLenBits                  = 3
	kNumHighLenBits                 = 8
	kNumLowLenSymbols               = 1 << kNumLowLenBits
	kNumMidLenSymbols               = 1 << kNumMidLenBits
	kNumLenSymbols                  = kNumLowLenSymbols + kNumMidLenSymbols + (1 << kNumHighLenBits)
	kMatchMaxLen                    = kMatchMinLen + kNumLenSymbols - 1
)

func stateInit() int {
	return 0
}

func stateUpdateChar(index int) int {
	if index < 4 {
		return 0
	}
	if index < 10 {
		return index - 3
	}
	return index - 6
}

func stateUpdateMatch(index int) int {
	if index < 7 {
		return 7
	}
	return 10
}

func stateUpdateRep(index int) int {
	if index < 7 {
		return 8
	}
	return 11
}

func stateUpdateShortRep(index int) int {
	if index < 7 {
		return 9
	}
	return 11
}

func stateIsCharState(index int) bool {
	if index < 7 {
		return true
	}
	return false
}

func getLenToPosState(length int) int {
	length -= kMatchMinLen
	if length < kNumLenToPosStates {
		return length
	}
	return kNumLenToPosStates - 1
}

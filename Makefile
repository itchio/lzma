include $(GOROOT)/src/Make.$(GOARCH)

TARG=compress/lzma
GOFILES=\
	lz_win.go\
	lzma_decoder.go\
	lzma_encoder.go\
	len_coder.go\
	lit_coder.go\
	range_bit_tree_coder.go\
	range_coder.go\

include $(GOROOT)/src/Make.pkg

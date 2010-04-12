include $(GOROOT)/src/Make.$(GOARCH)

TARG=compress/lzma
GOFILES=\
	lz_bin_tree.go\
	lz_in_window.go\
	lz_out_window.go\
	lzma_decoder.go\
	lzma_encoder.go\
	range_bin_tree_decoder.go\
	range_bin_tree_encoder.go\
	range_decoder.go\
	range_encoder.go\

include $(GOROOT)/src/Make.pkg

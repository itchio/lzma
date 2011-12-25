# Copyright (c) 2010, Andrei Vieru. All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

include $(GOROOT)/src/Make.inc

TARG=compress/lzma
GOFILES=\
	lz_window.go\
	lz_bin_tree.go\
	lzma_decoder.go\
	lzma_encoder.go\
	lzma_len_coder.go\
	lzma_lit_coder.go\
	range_bit_tree_coder.go\
	range_coder.go\
	util.go\

include $(GOROOT)/src/Make.pkg

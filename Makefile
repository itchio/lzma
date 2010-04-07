include $(GOROOT)/src/Make.$(GOARCH)

TARG=compress/lzma
GOFILES=\
	lzma_encoder.go\
	lzma_decoder.go\

include $(GOROOT)/src/Make.pkg

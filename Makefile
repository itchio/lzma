include $(GOROOT)/src/Make.$(GOARCH)

TARG=compress/lzma
GOFILES=\
	my1.go\

include $(GOROOT)/src/Make.pkg

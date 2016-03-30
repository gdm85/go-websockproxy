build:
	mkdir -p bin .gopath
	if [ ! -L .gopath/src ]; then ln -s "$(CURDIR)/vendor" .gopath/src; fi
	GOBIN="$(CURDIR)/bin/" GOPATH="$(CURDIR)/.gopath" go install

all: build

.PHONY: all build

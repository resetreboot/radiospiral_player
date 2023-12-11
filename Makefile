# Makefile for the RadioSpiral project
#

GO=go build
GO_OPTIONS=-buildmode=default


all: radiospiral

radiospiral: main.go radioplayer.go bundle.go
	$(GO) -o radiospiral $(GO_OPTIONS) main.go radioplayer.go bundle.go

# It's a phony so we can always call it and regenerate the file
.PHONY: generate
generate:
	go generate main.go

.PHONY: clean
clean:
	rm radiospiral

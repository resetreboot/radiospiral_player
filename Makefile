# Makefile for the RadioSpiral project
#

GO=go build
GO_OPTIONS=-buildmode=default


all: radiospiral

radiospiral: main.go acast.go radioplayer.go bundle.go
	$(GO) -o radiospiral $(GO_OPTIONS) main.go acast.go radioplayer.go bundle.go

win-radiospiral: main.go acast.go radioplayer.go bundle.go
	@env CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc GOOS=windows GOARCH=amd64 $(GO) -o radiospiral.exe $(GO_OPTIONS) main.go acast.go radioplayer.go bundle.go

arm-radiospiral: main.go acast.go radioplayer.go bundle.go
	@env CGO_ENABLED=1 CC=aarch64-linux-gnu-gcc GOOS=linux GOARCH=arm64 $(GO) -o radiospiral.exe $(GO_OPTIONS) main.go acast.go radioplayer.go bundle.go

# It's a phony so we can always call it and regenerate the file
.PHONY: generate
generate:
	go generate main.go

.PHONY: clean
clean:
	rm radiospiral

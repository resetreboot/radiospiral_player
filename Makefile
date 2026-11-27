# Makefile for the RadioSpiral project
#

GO=go build
GO_OPTIONS=-buildmode=default
WINDOWS_OPTIONS=-ldflags -H=windowsgui
SOURCES=main.go acast.go radioplayer.go bundle.go gui.go


all: radiospiral radiospiral.exe

radiospiral: $(SOURCES)
	@echo "Building Linux AMD64..."
	$(GO) -o radiospiral $(GO_OPTIONS) $(SOURCES)

radiospiral.exe: $(SOURCES)
	@echo "Building Windows AMD64..."
	@env CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc GOOS=windows GOARCH=amd64 $(GO) -o radiospiral.exe $(GO_OPTIONS) $(WINDOWS_OPTIONS) $(SOURCES)

radiospiral-arm64: $(SOURCES)
	@echo "Building Linux ARM64..."
	@env CGO_ENABLED=1 CC=aarch64-linux-gnu-gcc GOOS=linux GOARCH=arm64 $(GO) -o radiospiral-arm64 $(GO_OPTIONS) $(SOURCES)

# It's a phony so we can always call it and regenerate the file
.PHONY: generate
generate:
	go generate main.go

.PHONY: clean
clean:
	rm radiospiral
	rm radiospiral.exe
	rm radiospiral-arm64

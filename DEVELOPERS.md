# Instructions for developers

Minimum Go version: 1.21.4

It is recommended to use asdf managed Golang binaries

## Generation commands

We use a Makefile to build, generate and clean. 

* `make` or `make all` will compile the project
* `make generate` will update the binary `bundle.go` file
* `make clean` will remove the generated executable

## Setting up the development environment

Install MPlayer on your system. 

Ensure you are using the correct Golang version (we keep a
`.tools-version` in the repo that will prompt you if you
use asdf as your language version manager). 

Once done, make sure that version is running and install the
`fyne` utility: 

```
$ go install fyne.io/fyne/v2/cmd/fyne@latest
```

And then, on the source tree: 

```
go mod tidy
```

And you will be ready to generate, compile and run the program.

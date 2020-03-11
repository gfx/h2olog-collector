VERSION = $(shell godzil show-version)
CURRENT_REVISION = $(shell git rev-parse --short HEAD)
BUILD_LDFLAGS = "-s -w -X main.revision=$(CURRENT_REVISION)"


all: deps h2olog-collector
.PHONEY: all

deps: statik/statik.go
	go get -d
	go mod tidy
.PHONEY: deps

statik/statik.go:
	go get github.com/rakyll/statik
	statik -src=. -include='*.json'

h2olog-collector: statik/statik.go main.go
	go build -v

clean:
	rm -rf h2olog-collector statik
.PHONEY: clean
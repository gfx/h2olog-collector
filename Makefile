VERSION = $(shell godzil show-version)
CURRENT_REVISION = $(shell git rev-parse --short HEAD)
BUILD_LDFLAGS = "-s -w -X main.revision=$(CURRENT_REVISION)"


all: deps h2olog-collector
.PHONEY: all

crossbuild: deps
	GOOS=linux GOARCH=amd64 go build -o release/h2olog-collector
.PHONEY: crossbuild

deps: statik/statik.go
	go get -d
	go mod tidy
.PHONEY: deps

statik/statik.go:
	go get github.com/rakyll/statik
	statik -src=. -include='*.json'

h2olog-collector: statik/statik.go main.go
	go build -v

type.go:
	extract_h2olog_schema.pl 'h2olog.quic' ~/ghq/github.com/toru/h2olog/h2olog > type.go
	go fmt

clean:
	rm -rf h2olog-collector statik
.PHONEY: clean
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
	go run github.com/rakyll/statik -src=. -include='*.json'

h2olog-collector: statik/statik.go main.go
	go build -v

schema.sql: h2o-probes.d quicly-probes.d extract_h2olog_schema
	./extract_h2olog_schema ../../toru/h2olog h2olog.quic $@

h2o-probes.d:
	curl -sL https://raw.githubusercontent.com/h2o/h2o/master/h2o-probes.d > $@

quicly-probes.d:
	curl -sL https://raw.githubusercontent.com/h2o/quicly/master/quicly-probes.d > $@

clean:
	rm -rf h2olog-collector statik *.d
.PHONEY: clean
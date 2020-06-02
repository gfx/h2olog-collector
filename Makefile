VERSION = $(shell godzil show-version)
CURRENT_REVISION = $(shell git rev-parse --short HEAD)
BUILD_LDFLAGS = "-s -w -X main.revision=$(CURRENT_REVISION)"


all: deps h2olog-collector
.PHONY: all

release-linux: deps
	GOOS=linux GOARCH=amd64 go build -o release/h2olog-collector
.PHONY: release-linux

deps: statik/statik.go
	go get -d
	go mod tidy
.PHONY: deps

statik/statik.go:
	go get github.com/rakyll/statik
	go run github.com/rakyll/statik -src=. -include='*.json'

h2olog-collector: statik/statik.go main.go
	go build -v

schema: extract_h2olog_schema
	./extract_h2olog_schema ~/ghq/github.com/toru/h2olog h2olog.quic schema.sql
.PHONY: schema

test: h2olog-collector
	./h2olog-collector -dry-run -debug proj.h2olog.quic_test < test/test.jsonl
.PHONY: test

clean:
	rm -rf h2olog-collector statik *.d
.PHONY: clean
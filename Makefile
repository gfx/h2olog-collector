VERSION = $(shell godzil show-version)
CURRENT_REVISION = $(shell git rev-parse --short HEAD)
BUILD_LDFLAGS = "-s -w -X main.revision=$(CURRENT_REVISION)"


all: deps build/h2olog-collector build.linux-amd64/h2olog-collector
.PHONY: all

build.linux-amd64/h2olog-collector: deps go.mod main.go
	mkdir -p build.linux-amd64
	GOOS=linux GOARCH=amd64 go build -o $@

build/h2olog-collector: deps go.mod main.go
	mkdir -p build
	go build -o $@

deps: statik/statik.go
	go get -d -v
	go mod tidy -v
.PHONY: deps

update-deps:
	rm go.sum
	go get -u -v
	go mod tidy -v

statik/statik.go:
	go get github.com/rakyll/statik
	go run github.com/rakyll/statik -src=. -include='*.json'

schema: extract_h2olog_schema
	./extract_h2olog_schema ~/ghq/github.com/toru/h2olog h2olog.quic schema.sql
.PHONY: schema

test: build/h2olog-collector
	for n in {1..20} ; do echo "Testing #$n" ; ./build/h2olog-collector -dry-run -debug proj.h2olog.quic_test < test/test.jsonl ; done
.PHONY: test

clean:
	rm -rf build build.linux-amd64 statik *.d
.PHONY: clean

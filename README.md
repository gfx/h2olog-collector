# h2olog-collector

A log collector for [h2olog](https://github.com/toru/h2olog)

## Prerequisites

* Go compiler (>= 1.14)
* [h2olog](https://github.com/toru/h2olog)
* BigQuery table
* GCP authentication file named `autnh.json`


## Build

`make all` to build a binary for the current machine.

Or, you can use `make release-linux` to build a binary for Linux.

## Copyright

Copyright (c) 2019-2020 Fastly, Inc., FUJI Goro

See LICENSE for the liense.

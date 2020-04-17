package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cloud.google.com/go/bigquery"
	"github.com/rakyll/statik/fs"
	"google.golang.org/api/option"

	_ "github.com/gfx/h2olog-collector/statik"
)

const numWorkers = 16
const chanBufferSize = 5000
const tickDuration = 10 * time.Millisecond

var dryRun bool

type quicEvent = map[string]bigquery.Value

type valueSaver struct {
	row quicEvent
}

func (vs valueSaver) Save() (row map[string]bigquery.Value, insertID string, err error) {
	return vs.row, insertID, err
}

func millisToTime(millis int64) time.Time {
	sec := millis / 1000
	nsec := (millis - (sec * 1000)) * 1000000
	return time.Unix(sec, nsec).UTC()
}

func clientOption() option.ClientOption {
	statikFs, err := fs.New()
	if err != nil {
		log.Fatal(err)
	}

	r, err := statikFs.Open("/authn.json")
	if err != nil {
		log.Fatal(err)
	}
	defer r.Close()

	json, err := ioutil.ReadAll(r)
	if err != nil {
		log.Fatal(err)
	}

	return option.WithCredentialsJSON(json)
}

func readJSONLine(out chan valueSaver, reader io.Reader) {
	scanner := bufio.NewScanner(reader)

	for scanner.Scan() {
		line := scanner.Text()
		var row quicEvent

		d := json.NewDecoder(strings.NewReader(line))
		d.UseNumber()
		err := d.Decode(&row)
		if err != nil {
			log.Fatal(err)
		}

		iv, err := row["time"].(json.Number).Int64()
		if err != nil {
			log.Fatal(err)
		}
		row["time"] = millisToTime(iv)

		out <- valueSaver{row: row}
	}
}

func sleepForever() {
	select {}
}

func insertEvents(ctx context.Context, in chan valueSaver, inserter *bigquery.Inserter, id int) {
	rows := make([]valueSaver, 0)
	ticker := time.NewTicker(tickDuration)
	for range ticker.C {
		for len(in) > 0 {
			rows = append(rows, <-in)
		}

		if len(rows) > 0 {
			log.Printf("[%d] Insert rows (size=%d)", id, len(rows))

			if !dryRun {
				err := inserter.Put(ctx, rows)
				if err != nil {
					log.Fatal(err)
				}
			}
			rows = make([]valueSaver, 0)
		}
	}
}

func main() {
	flag.BoolVar(&dryRun, "dry-run", false, "Do not insert values into BigQuery")
	flag.Parse()

	if len(flag.Args()) != 1 {
		command := filepath.Base(os.Args[0])
		fmt.Printf("usage: %s [-dry-run] projectID.datasetID.tableID\n", command)
		os.Exit(0)
	}
	parts := strings.Split(flag.Arg(0), ".")
	projectID := parts[0]
	datasetID := parts[1]
	tableID := parts[2]

	ctx := context.Background()

	client, err := bigquery.NewClient(ctx, projectID, clientOption())
	if err != nil {
		log.Fatalf("bigquery.NewClient: %v", err)
	}
	defer client.Close()

	inserter := client.Dataset(datasetID).Table(tableID).Inserter()
	inserter.IgnoreUnknownValues = true
	inserter.SkipInvalidRows = false

	ch := make(chan valueSaver, chanBufferSize)
	defer close(ch)

	seq := make([]int, numWorkers)
	for i := range seq {
		go insertEvents(ctx, ch, inserter, i+1)
	}

	readJSONLine(ch, os.Stdin)
	sleepForever()
}

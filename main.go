package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"cloud.google.com/go/bigquery"
	"github.com/rakyll/statik/fs"
	"google.golang.org/api/option"

	_ "github.com/gfx/h2olog-collector/statik"
)

const numWorkers = 4
const chanBufferSize = 5000
const tickDuration = 10 * time.Millisecond

func (event quicEvent) Save() (row map[string]bigquery.Value, insertID string, err error) {
	v := reflect.Indirect(reflect.ValueOf(event))
	t := v.Type()

	row = make(map[string]bigquery.Value)

	for i := 0; i < v.NumField(); i++ {
		val := v.Field(i).Interface()
		name := t.Field(i).Tag.Get("json")
		if name == "at" {
			row[name] = millisToTime(val.(int64))
		} else {
			row[name] = val
		}
	}
	return row, insertID, err
}

//const iso8601Milli = "2006-01-02T15:04:05.000Z"

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

func readJSONLine(out chan quicEvent, reader io.Reader) {
	scanner := bufio.NewScanner(reader)

	for scanner.Scan() {
		line := scanner.Text()
		var row quicEvent
		json.Unmarshal([]byte(line), &row)

		out <- row
	}
}

func sleepForever() {
	select {}
}

func writeEvents(ctx context.Context, in chan quicEvent, inserter *bigquery.Inserter, id int) {
	rows := make([]quicEvent, 0)
	ticker := time.NewTicker(tickDuration)
	for range ticker.C {
		for len(in) > 0 {
			rows = append(rows, <-in)
		}

		if len(rows) > 0 {
			log.Printf("[%d] Insert rows (size=%d)", id, len(rows))

			err := inserter.Put(ctx, rows)
			if err != nil {
				log.Fatal(err)
			}
			rows = make([]quicEvent, 0)
		}
	}

}

func main() {
	if len(os.Args) < 2 {
		command := filepath.Base(os.Args[0])
		fmt.Printf("usage: %s projectID.datasetID.tableID\n", command)
		os.Exit(0)
	}
	parts := strings.Split(os.Args[1], ".")
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

	ch := make(chan quicEvent, chanBufferSize)
	defer close(ch)

	seq := make([]int, numWorkers)
	for i := range seq {
		go writeEvents(ctx, ch, inserter, i+1)
	}

	readJSONLine(ch, os.Stdin)
	sleepForever()
}

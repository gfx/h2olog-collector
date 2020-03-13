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

	// size of rows to insert at once
	size := 50

	ctx := context.Background()

	client, err := bigquery.NewClient(ctx, projectID, clientOption())
	if err != nil {
		log.Fatalf("bigquery.NewClient: %v", err)
	}
	defer client.Close()

	inserter := client.Dataset(datasetID).Table(tableID).Inserter()
	inserter.IgnoreUnknownValues = true
	inserter.SkipInvalidRows = false

	ch := make(chan quicEvent, size)
	defer close(ch)
	go readJSONLine(ch, os.Stdin)

	rows := make([]quicEvent, 0)
	tickDuration := 100 * time.Millisecond
	t := time.Now()

	ticker := time.NewTicker(tickDuration)
	for range ticker.C {
		for len(ch) > 0 {
			rows = append(rows, <-ch)
		}

		now := time.Now()
		if len(rows) > 0 && now.After(t.Add(tickDuration)) {
			log.Printf("Insert rows (size=%d)", len(rows))

			err := inserter.Put(ctx, rows)
			if err != nil {
				log.Fatal(err)
			}
			rows = make([]quicEvent, 0)
			t = now
		}
	}
}

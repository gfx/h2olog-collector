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
	"sync"
	"time"

	"cloud.google.com/go/bigquery"
	"github.com/rakyll/statik/fs"
	"google.golang.org/api/option"

	_ "github.com/gfx/h2olog-collector/statik"
)

const numWorkers = 16
const chanBufferSize = 5000
const bigQueryInsertSize = 5000
const tickDuration = 10 * time.Millisecond

var count uint64 = 0

var dryRun bool
var debug bool
var finished bool = false

type valueSaver struct {
	line string // a JSON string
	createdAt time.Time
}

func (vs valueSaver) Save() (row map[string]bigquery.Value, insertID string, err error) {
	row, err = decodeJSONLine(vs.line, vs.createdAt)
	return row, insertID, err
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

func decodeJSONLine(line string, createdAt time.Time) (row map[string]bigquery.Value, err error) {
	var rawEvent map[string]interface{}

	decoder := json.NewDecoder(strings.NewReader(line))
	decoder.UseNumber()
	err = decoder.Decode(&rawEvent)
	if err != nil {
		return nil, err
	}

	row = make(map[string]bigquery.Value)

	// "time" is stored as an epoch from 1970 in milliseconds,
	// so here it is converted to `time.Time` object.
	iv, err := rawEvent["time"].(json.Number).Int64()
	if err == nil {
		row["time"] = millisToTime(iv)
	}
	row["created_at"] = createdAt
	row["type"] = rawEvent["type"]
	row["seq"] = rawEvent["seq"]
	row["payload"] = line

	return row, nil
}

func readJSONLine(out chan valueSaver, reader io.Reader) {
	scanner := bufio.NewScanner(reader)

	for scanner.Scan() {
		line := scanner.Text()
		out <- valueSaver{line: line, createdAt: time.Now()}
	}
}

func insertEvents(ctx context.Context, latch *sync.WaitGroup, in chan valueSaver, inserter *bigquery.Inserter, id int) {
	defer func() {
		if debug {
			log.Printf("[%02d] Worker is finished", id)
		}
		latch.Done()
	}()

	rows := make([]valueSaver, 0)
	ticker := time.NewTicker(tickDuration)
	for range ticker.C {
		for len(in) > 0 && len(rows) < bigQueryInsertSize {
			select {
			case row := <-in:
				rows = append(rows, row)
			default:
			}
		}

		if len(rows) > 0 {
			if debug {
				log.Printf("[%02d] Insert rows (size=%d)", id, len(rows))
			}

			if !dryRun {
				err := inserter.Put(ctx, rows)
				if err != nil {
					log.Fatal(err)
				}
			} else {
				for _, row := range rows {
					v, _, _ := row.Save()
					b, err := json.Marshal(v)
					if err != nil {
						log.Fatal(err)
					}
					log.Printf("[%02d] %s", id, string(b))
				}
			}
			rows = make([]valueSaver, 0)
		} else if finished {
			break
		}
	}
}

func main() {
	var strict bool
	flag.BoolVar(&strict, "strict", false, "Turn IgnoreUnknownValues and SkipInvalidRows off")
	flag.BoolVar(&dryRun, "dry-run", false, "Do not insert values into BigQuery")
	flag.BoolVar(&debug, "debug", false, "Emit debug logs to STDERR")
	flag.Parse()

	if len(flag.Args()) != 1 {
		command := filepath.Base(os.Args[0])
		fmt.Printf("usage: %s [-strict] [-dry-run] [-debug] projectID.datasetID.tableID\n", command)
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
	inserter.IgnoreUnknownValues = !strict
	inserter.SkipInvalidRows = !strict

	ch := make(chan valueSaver, chanBufferSize)
	defer close(ch)

	seq := make([]int, numWorkers)
	latch := &sync.WaitGroup{}

	for i := range seq {
		latch.Add(1)
		go insertEvents(ctx, latch, ch, inserter, i+1)
	}

	readJSONLine(ch, os.Stdin)
	finished = true
	latch.Wait()
}

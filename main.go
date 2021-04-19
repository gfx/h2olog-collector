package main

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/bigquery"
	_ "cloud.google.com/go/storage"
	"google.golang.org/api/option"
)

const numWorkers = 16
const chanBufferSize = 5000
const bigQueryInsertSize = 5000
const tickDuration = 10 * time.Millisecond

var count uint64 = 0

var dryRun bool
var debug bool
var finished bool = false

//go:embed authn.json
var authnJson []byte

type authnCredentials struct {
	ProjectID string `json:"my_project"`
}

type valueSaver struct {
	line      string // a JSON string
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
	return option.WithCredentialsJSON(authnJson)
}

func getProjectID() string {
	var authn authnCredentials
	err := json.Unmarshal(authnJson, &authn)
	if err != nil {
		panic("No project_id in authn.json")
	}
	return authn.ProjectID
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

func insertEvents(ctx context.Context, latch *sync.WaitGroup, in chan valueSaver, bqInserter *bigquery.Inserter, id int) {
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
				// BigQuery
				if bqInserter != nil {
					err := bqInserter.Put(ctx, rows)
					if err != nil {
						// TODO: retry
						log.Fatal(err)
					}
				}
				// TODO: GCS
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

func usage() {
	command := filepath.Base(os.Args[0])
	fmt.Printf("usage: %s [-strict] [-dry-run] [-debug] [-bq=datasetID.tableID] [-gcs=bucketID]\n", command)
}

func main() {
	var strict bool
	var bq string  // datasetID.tableID
	var gcs string // bucketID

	flag.BoolVar(&strict, "strict", false, "Turn IgnoreUnknownValues and SkipInvalidRows off")
	flag.BoolVar(&dryRun, "dry-run", false, "Do not insert values into BigQuery")
	flag.BoolVar(&debug, "debug", false, "Emit debug logs to STDERR")
	flag.StringVar(&bq, "bq", "", "Insert logs into BigQuery with datasetID.tableID")
	flag.StringVar(&gcs, "gcs", "", "Insert logs into Google Cloud Storage with bucketID")
	flag.Parse()

	if len(flag.Args()) != 0 {
		usage()
		os.Exit(1)
	}

	var bqDatasetID string
	var bqTableID string
	if bq != "" {
		parts := strings.Split(bq, ".")
		if len(parts) != 2 {
			usage()
			os.Exit(1)
		}
		bqDatasetID = parts[0]
		bqTableID = parts[1]
	}

	ctx := context.Background()
	projectID := getProjectID()

	// setup BigQuery inserter
	var bqClient *bigquery.Client;
	var bqInserter *bigquery.Inserter = nil
	if bq != "" {
		log.Printf("setup BigQuery client")
		var err error
		bqClient, err = bigquery.NewClient(ctx, projectID, clientOption())
		if err != nil {
			log.Fatalf("bigquery.NewClient: %v", err)
		}
		defer bqClient.Close()
		bqInserter = bqClient.Dataset(bqDatasetID).Table(bqTableID).Inserter()
		bqInserter.IgnoreUnknownValues = !strict
		bqInserter.SkipInvalidRows = !strict
	}

	// TODO setup GCS

	ch := make(chan valueSaver, chanBufferSize)
	defer close(ch)

	seq := make([]int, numWorkers)
	latch := &sync.WaitGroup{}

	for i := range seq {
		latch.Add(1)
		go insertEvents(ctx, latch, ch, bqInserter, i+1)
	}

	readJSONLine(ch, os.Stdin)
	finished = true
	latch.Wait()
}

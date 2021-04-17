package main

import (
	"bufio"
	"context"
	_ "embed"
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
	"cloud.google.com/go/storage"
	json "github.com/goccy/go-json"
	lru "github.com/hashicorp/golang-lru"
	"google.golang.org/api/option"
)

const numWorkers = 16
const chanBufferSize = 5000
const bigQueryInsertSize = 5000
const tickDuration = 10 * time.Millisecond
const connToLogsLruMapSize = 10000

var count uint64 = 0
var isReachedToEOF bool = false // the input stream is EOF

var dryRun bool         // -dry-run
var debug bool          // -debug
var numberOfPackets int // -number-of-packets=N

// per-connection logs for GCS
// conn_id -> array of raw logs
var connToLogs = newLruMap(connToLogsLruMapSize)

//go:embed authn.json
var authnJson []byte

type authnCredentials struct {
	ProjectID string `json:"project_id"`
}

type h2ologEvent struct {
	rawJsonLine string
	rawEvent    map[string]interface{}
	createdAt   time.Time // timestamp added in h2olog-collector
}

// it implements bigquery.ValueSaver
type valueSaver struct {
	payload map[string]bigquery.Value
}

func (vs valueSaver) Save() (row map[string]bigquery.Value, insertID string, err error) {
	row = vs.payload
	return row, insertID, err
}

func millisToTime(millis int64) time.Time {
	sec := millis / 1000
	nsec := (millis - (sec * 1000)) * 1000000
	return time.Unix(sec, nsec).UTC()
}

func newLruMap(n int) *lru.Cache {
	lruMap, err := lru.New(n)
	if err != nil {
		panic(err)
	}
	return lruMap
}

func getClientOption() option.ClientOption {
	return option.WithCredentialsJSON(authnJson)
}

func getProjectID() string {
	var authn authnCredentials
	err := json.Unmarshal(authnJson, &authn)
	if err != nil || authn.ProjectID == "" {
		panic("No project_id in authn.json")
	}
	return authn.ProjectID
}

func decodeJSONLine(line string) (map[string]interface{}, error) {
	var rawEvent map[string]interface{}
	decoder := json.NewDecoder(strings.NewReader(line))
	decoder.UseNumber()
	err := decoder.Decode(&rawEvent)
	if err != nil {
		return nil, err
	}
	return rawEvent, nil
}

func newBqRow(ev h2ologEvent) (map[string]bigquery.Value, error) {
	var row map[string]bigquery.Value

	// "time" is stored as an epoch from 1970 in milliseconds,
	// so here it is converted to `time.Time` object.
	iv, err := ev.rawEvent["time"].(json.Number).Int64()
	if err == nil {
		row["time"] = millisToTime(iv)
	}
	row["created_at"] = ev.createdAt
	row["type"] = ev.rawEvent["type"]
	row["seq"] = ev.rawEvent["seq"]
	row["payload"] = ev.rawEvent
	return row, nil
}

func readJSONLine(out chan h2ologEvent, reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()
		rawEvent, err := decodeJSONLine(line)
		if err != nil {
			log.Printf("decode error: %v", err)
		} else {
			out <- h2ologEvent{
				rawJsonLine: line,
				rawEvent:    rawEvent,
				createdAt:   time.Now(),
			}
		}
	}
}

func insertEventsToBQ(ctx context.Context, latch *sync.WaitGroup, in chan h2ologEvent, bqInserter *bigquery.Inserter, workerID int) {
	defer func() {
		if debug {
			log.Printf("[%02d] Worker is finished", workerID)
		}
		latch.Done()
	}()

	rows := make([]valueSaver, 0)
	ticker := time.NewTicker(tickDuration)
	for range ticker.C {
		for len(in) > 0 && len(rows) < bigQueryInsertSize {
			select {
			case row := <-in:
				payload, err := decodeJSONLine(row.rawEvent, row.createdAt)
				if err != nil {
					log.Printf("Cannot decode JSON lines: %v", err)
					break
				}
				rows = append(rows, valueSaver{payload: payload})
			default:
			}
		}

		if len(rows) > 0 {
			if debug {
				log.Printf("[%02d] Insert rows (size=%d)", workerID, len(rows))
			}

			if !dryRun {
				err := bqInserter.Put(ctx, rows)
				if err != nil {
					// TODO: retry
					log.Fatal(err)
				}
			} else { // dry-run
				for _, row := range rows {
					v, _, _ := row.Save()
					b, err := json.Marshal(v)
					if err != nil {
						log.Fatal(err)
					}
					log.Printf("[%02d] %s", workerID, string(b))
				}
			}
			rows = make([]valueSaver, 0)
		} else if isReachedToEOF {
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
	var bqID string        // datasetID.tableID
	var gcsBucketID string // bucketID

	flag.BoolVar(&strict, "strict", false, "Turn IgnoreUnknownValues and SkipInvalidRows off")
	flag.BoolVar(&dryRun, "dry-run", false, "Do not insert values into BigQuery")
	flag.BoolVar(&debug, "debug", false, "Emit debug logs to STDERR")
	flag.StringVar(&bqID, "bq", "", "Insert logs into BigQuery with datasetID.tableID")
	flag.StringVar(&gcsBucketID, "gcs", "", "Insert logs into Google Cloud Storage with bucketID")
	flag.IntVar(&numberOfPackets, "number-of-packets", 1000, "Number of packets to record")
	flag.Parse()

	if len(flag.Args()) != 0 {
		usage()
		os.Exit(1)
	}

	if bqID == "" && gcsBucketID == "" {
		log.Fatalf("Neither BigQuery tableID (-bq) nor GCS bucketID (-gcs) are specified")
	}

	ctx := context.Background()
	projectID := getProjectID()
	clientOption := getClientOption()

	// setup BigQuery inserter
	var bqClient *bigquery.Client
	var bqInserter *bigquery.Inserter = nil
	if bqID != "" {
		log.Printf("setup BigQuery client")

		var bqDatasetID string
		var bqTableID string
		parts := strings.Split(bqID, ".")
		if len(parts) != 2 {
			usage()
			os.Exit(1)
		}
		bqDatasetID = parts[0]
		bqTableID = parts[1]

		var err error
		bqClient, err = bigquery.NewClient(ctx, projectID, clientOption)
		if err != nil {
			log.Fatalf("bigquery.NewClient: %v", err)
		}
		defer bqClient.Close()
		bqInserter = bqClient.Dataset(bqDatasetID).Table(bqTableID).Inserter()
		bqInserter.IgnoreUnknownValues = !strict
		bqInserter.SkipInvalidRows = !strict
	}

	// setup GCS
	var gcsClient *storage.Client
	var gcsBucket *storage.BucketHandle
	if gcsBucketID != "" {
		var err error
		gcsClient, err = storage.NewClient(ctx, clientOption)
		if err != nil {
			log.Fatalf("storage.NewClient: %v", err)
		}
		defer gcsClient.Close()
		gcsBucket = gcsClient.Bucket(gcsBucketID)
	}

	bqCh := make(chan h2ologEvent, chanBufferSize)
	defer close(bqCh)
	gcsCh := make(chan h2ologEvent, chanBufferSize)
	defer close(gcsCh)

	latch := &sync.WaitGroup{}

	for i := range make([]int, numWorkers) {
		latch.Add(1)
		if bqInserter != nil {
			go insertEventsToBQ(ctx, latch, bqCh, bqInserter, i+1)
		}
		if gcsBucket != nil {
			// TODO
		}
	}

	readJSONLine(bqCh, os.Stdin)
	isReachedToEOF = true
	latch.Wait()
}

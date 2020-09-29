create table `h2olog.events` (
  `type` string, -- event type
  `seq` int64, -- a sequence number, provided by h2olog
  `time` timestamp, --  provided by h2o
  `created_at` timestamp, -- provided by h2olog-collector

  `payload` string, -- JSON payload
)
partition by date(`created_at`);

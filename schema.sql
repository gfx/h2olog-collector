
create table `h2olog.quic` (
  `type` string,
  `ordering` int64, -- a sequence number added by h2olog-collector
  `seq` int64, -- a sequence number added by `h2olog`

  `lost` int64, -- type="h2olog-event-lost"
  `conn` int64,
  `time` timestamp,
  `version` int64,
  `dcid` string,
  `state` int64,
  `bytes_len` int64,
  `new_version` int64,
  `pn` int64,
  `decrypted_len` int64,
  `ret` int64,
  `is_enc` int64,
  `epoch` int64,
  `label` string,
  `phase` int64,
  `next_pn` int64,
  `first_octet` int64,
  `len` int64,
  `ack_only` int64,
  `newly_acked` int64,
  `inflight` int64,
  `cwnd` int64,
  `pto_count` int64,
  `largest_acked` int64,
  `bytes_acked` int64,
  `max_lost_pn` int64,
  `error_code` int64,
  `frame_type` int64,
  `reason_phrase` string,
  `stream_id` int64,
  `off` int64,
  `is_fin` int64,
  `limit` int64,
  `is_unidirectional` int64,
  `token` string,
  `generation` int64,
  `packet_type` int64,
  `fin` int64,
  `ack_block_begin` int64,
  `ack_block_end` int64,
  `ack_delay` int64,
  `min_rtt` int64,
  `smoothed_rtt` int64,
  `variance_rtt` int64,
  `latest_rtt` int64,
  `conn_id` int64,
  `req_id` int64,
  `name` string,
  `value` string
);

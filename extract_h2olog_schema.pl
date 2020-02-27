#!/usr/bin/env perl
# usage: extract_h2olog_schema.pl 'h2olog.quic' ~/ghq/github.com/toru/h2olog/h2olog.py

use strict;
use warnings FATAL => "all";
use feature 'say';

my $table_name = shift @ARGV;
unless (defined $table_name) {
  die "usage: $0 table_name path/to/h2olog\n";
}

while (my $line = <>) {
  last if $line =~ /\bstruct quic_event_t\b/;
}

my @sql_columns;
my @go_fields;

while (my $line = <>) {
  last if $line =~ /}/;
  next if $line =~ qr{^\s*//};

  my($type, $name, $is_array) = $line =~ /(\w+)\s+(\w+)(\[.*?\])?/ or next;

  my $bq_type;
  my $go_type;
  if ($name eq "at") {
    $bq_type = "timestamp";
    $go_type = "int64";
  } elsif ($type eq "char" && $is_array) {
    $bq_type = "string";
    $go_type = "string";
  } elsif ($type =~ /^(?<sign>[iu])(?<bits>\d+)$/) {
    $bq_type = "int64";
    $go_type = "int64";
  } else {
    die "unknown type: $type";
  }

  push @sql_columns, "`$name` $bq_type";

  my $go_name = camelize($name);
  push @go_fields, qq{$go_name $go_type `json:"$name"`};
}

say "-- BigQuery DDL";
say "create table `$table_name` (";
say "  ", join ",\n  ", @sql_columns;
say ");";

say "\n";

say "// Go";
say "type quicEvent struct {";
# say qq{  InsertID string `json:"insertId"`\n};
say "  ", join "\n  ", @go_fields;
say "}";

sub camelize {
    my ($s) = @_;
    return join "", map { uc($_) eq "ID" ? "ID" : ucfirst($_) } split /_/, $s;
}

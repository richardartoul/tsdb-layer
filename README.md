# TSDB Layer

# Overview

The purpose of this project is to serve as a proof of concept time series database layer based on [FoundationDB](https://www.foundationdb.org/). While the project could eventually be evolved into a production-ready system, the current implementation is optimized for rapid prototyping and should not be used in production.

# Goals

1. High throughput writes (500k+ datapoints / second / storage node).
2. High levels of compression ([Gorilla / TSZ style](https://www.vldb.org/pvldb/vol8/p1816-teller.pdf)).
3. Moderate throughput reads (10k+ time series / second / storage node).

# Non-Goals

1. Extremely low latency. While latency should be minimized as much as possible, throughput will usually take precedence.
2. Query language.

# Stretch Goals

1. Production readiness.
2. High availability.
3. Time series indexing.
4. Aggregation.

# Design

## Naive Design #1 - One Write/Datapoint

The most naive implementation of this layer would be to implement it the way the [FoundationDB documentation recommends modeling time series data](https://apple.github.io/foundationdb/time-series.html).

### Pros

1. Stateless
2. Simple implementation

### Cons

1. FoundationDB is limited in the number of writes/s it can perform, especially against the SSD engine. Even with writes batched together into the same transaction, benchmarking on a 2017 i7 macbook pro demonstrated that the system struggled to handle more than a few thousand datapoints/s.
2. Extremely poor / no compression.

## Naive Design #2 - Streaming TSZ Compression in FoundationDB

Another implementation that was attempted to solve the lack of compression in #1 was to store compressed chunks of TSZ compressed data in fdb. Every time a new datapoint arrived, the most recent compressed chunk would be read out of FoundationDB (along with some additional metadata) which would then be used to generate a new compressed chunk that included the new data point.

### Pros

1. Stateless
2. (Relatively) simple implementation

### Cons

1. 1. FoundationDB is limited in the number of writes/s it can perform, especially against the SSD engine. Even with writes batched together into the same transaction, benchmarking on a 2017 i7 macbook pro demonstrated that the system struggled to handle more than a few thousand datapoints/s. In addition, each write required a prior read which limited throughput even further.

## Design #3 (current) - Stateful TSDB using FoundationDB as the storage layer.

The current design is the most complicated but also the most efficient. The idea is to build a stateful (memory) system in front of FoundationDB. One way to explain this design is that it is the same as [M3DB](https://github.com/m3db/m3) except it uses fdb instead of a filesystem / disk. Another way to think of it is that the layer functions as the "memtable" and fdb is used for storing the commitlog and "sstables".

Specifically as writes arrive into the system they are stored and compressed in-memory. In addition, they are written to fdb as part of a commitlog entry for durability. In the background, as compressed time series blocks accumulate in memory they are flushed to fdb as compressed blocks on a per time series basis.

At first blush it would seem like this approach would have all of the same issues as designs #1 and #2 in terms of write throughput, however, fdb's write throughput limitations are mitigated in this case by batching multiple logical writes together into a single physical write in the form of a "commitlog chunk". This is possible because the commit log chunks don't need to be readable in an online manner. Their only purpose is to allow a node that has been replaced / restarted to recover any in-memory state that had not yet been flushed as compressed (readable) blocks.

TODO(rartoul): Fill in the rest.


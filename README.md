# Time Series and FoundationDB: How I leveraged FoundationDB to achieve millions of writes per second and 10x compression in under two thousand lines of Go code

## Disclaimer

I want to preface everything you’re about to read with the disclaimer that I built DiamondDB purely as a PoC to measure performance of different architectures for storing time series data in FoundationDB. It is in no way production ready. In fact, the code is littered with TODOs, cut corners, and missing features. The only thing DiamondDB is useful for in its current form is demonstrating how a performant time series database **could** be built on-top of FDB and reminding me that I should go outside more often. If you want a distributed database with the functionality described in this blog post, you should just use [M3DB](https://github.com/m3db/m3) itself.

## Target Audience

This blog post is targeted at engineers who either work on large-scale distributed systems, or are curious about them.

Throughout this post we’ll look at the problem of storing high volume time series data to illustrate how FoundationDB's excellent performance characteristics and strong consistency guarantees can be utilized to build reliable and performant distributed systems.

In this case we're going to build a distributed time series database (modeled after [M3DB](https://github.com/m3db/m3)) that can handle millions of writes/s (with best in class compression!) on my 2018 Macbook pro in less than 2,000 lines of Go code.

## High Volume Time Series Data

At $DAYJOB I spend most of my time developing an open-source distributed time series database called [M3DB](https://github.com/m3db/m3). So naturally my first instinct was to see if I could replicate an M3DB-like system using FDB.

Time series means different things to different people. In this case, I want to focus on the type of time series storage engine that could efficiently power an [OLTP](https://en.wikipedia.org/wiki/Online_transaction_processing) system (strong consistency and immediately read your writes) or a monitoring / observability workload as opposed to a time series database designed for [OLAP](https://en.wikipedia.org/wiki/Online_analytical_processing) workloads.

Primarily, our system should support the following two APIs:

```golang
type Value struct {
  Timestamp int64
  Value float64
}

Write(seriesID string, value Value)

Read(seriesID) ([]Value)
```

Note that M3DB has support for several other important features, such as custom types and inverted indexing, but let's put that aside for a moment.

At this point you may be wondering why do we even need a fancy distributed system in the first place? Can’t we easily solve this problem using PostgreSQL with a simple table schema? An example being:

```sql
CREATE TABLE timeseries (
  series_id TEXT,
  timestamp integer,
  value double precision,
  PRIMARY KEY(series_id, timestamp)
);
```

This implementation would work for some small use-cases, but M3DB has three properties that the Postgres implementation does not:

1. Horizontal scalability (as additional machines are added the throughput of the system should increase in a roughly linear fashion)
2. High write throughput (millions of writes/s)
3. Efficient compression

Its possible for PostGres to partially address the compression requirement in a variety of ways. ([This gist](https://gist.github.com/richardartoul/23b66ea6924f28fc6ec8dfcd06901302) is an example that demonstrates how a stored procedure can be used to perform time series compression, but the compression will never be as good as a custom designed algorithm, such as [Gorilla](https://www.vldb.org/pvldb/vol8/p1816-teller.pdf). In addition, the Postgres implementation will never achieve horizontal scalability or high write throughput without application layer sharding.

If you're wondering why you would ever need to store so much data that you wouldn't be able to fit it all in a single large Postgres instance, consider the monitoring / observability use-case. Imagine you have a fleet of `50,000` servers and you want to monitor `100` different metrics (free disk space, CPU utilization, etc.) about each one at `10s` intervals. This would generate `5,000,000` unique time series and `500,000` data points per second, and you're still not even tracking any application level metrics!

Implementing the `Write` and `Read` interfaces, while also achieving the three properties listed above, is the crux of what M3DB and other distributed time series databases in this space seek to accomplish.

## A Software Foundation for Distributed Systems

FoundationDB (FDB) first piqued I first started paying attention to FoundationDB when I listened to a [podcast](https://www.dataengineeringpodcast.com/foundationdb-distributed-systems-episode-80/) during which [Ryan Worl](https://twitter.com/ryanworl) explained how FDB can be used as an extremely powerful primitive for building distributed systems. This piqued my interest because distributed systems engineers are **severely** lacking in good primitives.

But why does FDB make for such a good primitive? To answer that question, we first need to understand the data model of FoundationDB. FoundationDB is a distributed system that provides the following semantics:

1. Key/Value storage where keys and values can be arbitrary byte arrays.
2. Keys are "sorted" lexicographically such that reading and truncating large sorted ranges is efficient.
3. Automatic detection and redistribution of hot keys (this one is particularly unique and I’m not aware of many other systems that handle this gracefully).
4. Completely ACID transactions at the highest level of isolation `strict serializability` across arbitrary key/value pairs.

This is basically the holy grail of primitives for building distributed systems. For example, the architecture of almost every "distributed sql" database on the market right now boils down to some (admittedly really hairy) logic for dealing with SQL and transactions wrapped around a distributed key/value store:

- [Exhibit A](https://pingcap.com/docs/v3.0/architecture/)
- [Exhibit B](https://github.com/cockroachdb/cockroach/blob/master/docs/design.md)

While there are other systems out there that offer similar semantics as FoundationDB does, FDB is notable for the fact that it was designed from the ground up with the idea of building other distributed systems on top of it and this decision permeates the entire system, from its architecture and documentation to its performance characteristics and APIs.

On top of that, its impossible to spend any amount of time with FDB and not come away with a deep appreciation for the level of careful consideration and engineering that went into it.

The path to distributed systems hell is paved with good ideas ruined by mediocre and poorly tested implementations, and there is nothing mediocre or poorly tested about FoundationDB.

Of course I don't want to spend this entire blog post gushing about how amazing FoundationDB is (although it really is quite good) so if you want to learn more about it here are some resources to get started:

- [(Video) Technical Overview of FoundationDB](https://www.youtube.com/watch?v=EMwhsGsxfPU)

- [The Docs](https://apple.github.io/foundationdb/#documentation)

Now that we have established some much needed context, let’s switch gears and actually build something!

## The Design and Implementation of DiamondDB

The question I wanted to explore was this: Could I build a system with the same API, compression, and performance characteristics as M3DB, but as a thin layer on-top of FDB instead of a custom distributed system written from the ground up with its own storage engine (as M3DB is).

The most naive approach to storing timeseries data in FDB looks something like this:

```golang
db.Transact(func(tr FDB.Transaction) (interface{}, error) {
  for _, w := range writes {
    key := tuple.Tuple{w.ID, w.Timestamp.UnixNano()}
    tr.Set(key, tuple.Tuple{w.Value}.Pack())
  }
  return nil, nil
})
```

Each datapoint is stored as an individual record in FDB where the key is a tuple in the form `<time_series_id, unix_timestamp>` and the value is a tuple in the form `<value>`.

FDB keys are sorted so we can "efficiently" query for all the values for a given series by issuing a prefix query for all keys that begin with the specified time series ID.

This design has several issues:

1. Compression is terrible because the time series ID is repeated for each record. This could be addressed by assigning each time series a unique integer ID so that each time series ID would only be stored once and all the datapoint entries would reference the integer. This is equivalent to a foreign key relationship in traditional relational databases and is easy to implement because of FDB’s strong transactional semantics, however, compression would still be poor compared to modern time series databases’ as we'd still have to store the timestamp (8 bytes) and value (8 bytes) in their entirety, plus an additional 8 bytes for the time series ID "pointer" (assuming we used an unsigned 64 bit integer).

2. Write throughput is terrible because every write to FDB is a real transaction. Benchmarking on my laptop indicated that getting more than a few thousand writes per second per storage node on commodity hardware using the `ssd` engine would be difficult. We could use the `memory` engine which is much faster while still being durable, however, that requires the entire working set of the database to fit in memory of all the storage nodes which is a constraint I didn’t want to impose on this project since RAM is much more expensive than disk.

I didn’t expect this design to work, but its always good to benchmark the simple approach first so you can measure exactly how much of an improvement you’re getting with the more complex solution and weigh the benefits of complexity vs. performance.

The next design I attempted was to perform [Gorilla Compression](https://www.vldb.org/pvldb/vol8/p1816-teller.pdf) on the time series data. This turns out to be tricky because Gorilla compression is usually performed in memory since it involves writing individual bits at a time. Despite this obstacle I was able to implement a prototype where each write was performed by loading the current state of a Gorilla encoder out of FDB, encoding the new value into the (now in-memory) encoder, and then finally writing the state of the encoder back to FDB.

Here is a simplified version of the primary FDB transaction for this implementation:

```golang
_, err := l.db.Transact(func(tr fdb.Transaction) (interface{}, error) {
  metadataKey := newTimeseriesMetadataKeyFromID(write.seriesID)
  metadata, err := tr.Get(metadataKey).Get()
  if err != nil {
    return nil, err
  }

  var (
    metaValue  timeSeriesMetadata
    dataAppend []byte
    enc         = encoding.NewEncoder()
  )

  if len(metadataBytes) == 0 {
    // Never written.
    enc := encoding.NewEncoder()
    if err := enc.Encode(write.Timestamp, write.Value); err != nil {
      return nil, err
    }

    metaValue = timeSeriesMetadata{
      State: enc.State(),
    }

    b := enc.Bytes()
    if len(b) > 1 {
      dataAppend = enc.Bytes()[:len(b)-1]
    }
  } else {
    if err := json.Unmarshal(metadataBytes, &metaValue); err != nil {
      return nil, err
    }

    // Has been written before, restore encoder state.
    if err := enc.Restore(metaValue.State); err != nil {
      return nil, err
    }

    if err := enc.Encode(write.Timestamp, write.Value); err != nil {
      return nil, err
    }

    // Ensure new state gets persisted.
    var (
      newState = enc.State()
      b        = enc.Bytes()
    )
    if len(b) == 0 {
      return nil, errors.New("encoder bytes was length zero")
    }
    if len(b) == 1 {
      // The existing last byte was modified without adding any additional bytes. The last
      // byte is always tracked by the state so there is nothing to append here.
    }
    if len(b) > 1 {
      // The last byte will be kept track of by the state, but any bytes preceding it are
      // new "complete" bytes which should be appended to the compressed stream.
      dataAppend = b[:len(b)-1]
    }
    metaValue.LastByte = b[len(b)-1]
    metaValue.State = newState
  }

  newMetadataBytes, err := json.Marshal(&metaValue)
  if err != nil {
    return nil, err
  }

  tr.Set(metadataKey, newMetadataBytes)
  dataKey := newTimeseriesDataKeyFromID(write.ID)
  tr.AppendIfFits(dataKey, dataAppend)

  return nil, nil
})
```

Note that Gorilla compression operates at the bit (not byte) level so some care had to be taken to manage the last byte of the compressed stream (which could be partial).

This implementation provided compression levels as good as any modern time series database, but still suffered from terrible write throughput. I couldn't get more than ~5000 writes per second on my laptop using this implementation which makes a lot of sense considering that even though sometimes I was only adding a bit or two to a compressed stream, FDB still had to read/write an entire page of data to add those two additional bits of information since it uses a modified version of SQLite as its storage engine.

The conclusion I came to was that in order to achieve high write throughput I'd have to implement a semi-stateful system in front of FDB. I'd been trying to avoid doing this since it's much more complicated than implementing a simple stateless layer, but in the words of Spiderman: "With great scale comes great complexity".

What do I mean by "semi-stateful"? Let’s start by looking at the architecture of a truly stateful system, such as M3DB.

M3DB's storage system behaves similar to a [Log Structured Merge Tree](https://en.wikipedia.org/wiki/Log-structured_merge-tree), except instead of compacting based on levels, compaction is based on time.

![](./resources/m3db_storage.png)

To put that into concrete terms, as M3DB accepts writes they're immediately written to the commit log for durability. This ensures that all acknowledged writes can always be recovered in the case of a crash or failure. At the same time, incoming writes are also buffered in memory where they are actively compressed using Gorilla encoding.

At regular intervals, data that has been compressed in memory is flushed to disk as immutable files (merging with any existing files if they already exist) where each set of files contains all of the data for all of the values during a fixed "block" period. For example, if the blocksize is configured to 2 hours then a set of files would contain all values with timestamps between 12pm and 2pm.

This architecture allows M3DB to achieve high levels of compression AND also achieve high write throughput (since writes only need to be buffered in memory and written to a commitlog file before being acknowledged). The only caveat is that if an M3DB node fails (for whatever reason) when it then starts back up it will first need to read the commitlog in its entirety and rebuild the pre-failure in-memory state before it can begin serving reads. This can take some time.

Another way to understand M3DB’s architecture is that at any point in time an acknowledged write must live in either an immutable fileset **or** a mutable encoder **and** a commit log file.

![](./resources/m3db_time.png)

I decided that if I was going to achieve similar levels of performance as M3DB that I would need to replicate the architecture as well.

![](./resources/fdb_storage.png)

Notice that the architecture looks very similar to M3DB's, except instead of using the filesystem we use FDB. This is why I refer to the architecture as "semi-stateful". It's stateful in the sense that it needs to hold some state in memory and if a node fails or reboots it will have to "bootstrap" that state from the commit logs just like M3DB does.

However, since the commit logs and compressed data blocks are stored in FDB we don't have to worry about storage state. This is important because it greatly simplifies operational concerns. For example, imagine we wanted to run our database on Kubernetes. Accomplishing this with a completely stateful system like M3DB requires using [StatefulSets](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/), [Persistent Volumes](https://kubernetes.io/docs/concepts/storage/persistent-volumes/), and also writing an [operator](https://github.com/m3db/m3db-operator) to manage it all. With this implementation where the storage is backed by FDB, running this on Kubernetes would be much more straightforward. As long as we had some way to assign each instance of our database a unique identifier, Kubernetes would be free to move the instances around since each instance could simply bootstrap itself from FDB after being moved.

Of course, all of this relies upon the fact that you're able to maintain and operate an FDB cluster, but that's the point of building on top of FDB. Once you've figured out how to setup and operate FDB clusters, you can build all of your other distributed systems on top of it and let FDB handle the most complicated portions of the distributed systems so you can focus on the portions that are unique to the problem you're trying to solve.

Let’s examine the implementation of this architecture in more detail, starting with the commit logs. If you want to read the code yourself, you can find it [here](https://github.com/richardartoul/tsdb-layer/blob/master/src/layer/rawblock/commitlog.go). The general idea with the commitlog is that we need to batch many writes together and encode them into a binary format that can be decoded in a linear fashion quickly. The commitlog format does **not** need to support random reads in an efficient manner.

Implementing this is rather straightforward, we just need to gather writes together and then send them in large batches to FDB. Most of the code linked above concerns itself with making sure we can do this in a performant manner, as well as concurrent signaling (since we can't acknowledge writes back to the caller until a commitlog "chunk" containing all of their writes has been persisted in FDB).

The other requirement of the commit log chunks is that we need to be able to:

1. Fetch all undeleted commitlog chunks from FDB (required to "bootstrap" an instance after a restart/failure/reschedule)
2. Delete all commitlog chunks before a provided chunk (the reason for this will become clear in a minute)

Luckily, both of these operations are relatively efficient in FDB as long as the keys are formatted correctly. Remember, the abstraction provided by FDB is that of a sorted key/value store, so we just need to format the keys such that they sort lexicographically in a way that makes the two operations described above efficient.

The way we accomplish this is very straightforward. We use FDB's [tuple layer](https://apple.github.io/foundationdb/data-modeling.html#tuples) to generate keys in the form `<commitlog, COMMITLOG_INDEX>`, for example: `<commitlog, 0>` would be the key for the first chunk. The `commitlog` prefix is used to separate the commitlog from entries for other portions of the system, and the index number provides a monotonically increasing number that can be used so that we can perform operations like: "delete all commitlog chunks before chunk number #127".

The storage engine will be writing out commitlog chunks constantly so they need to be cleaned up regularly. But how do we know when it's safe to delete a given chunk? One easy way to do that is to take advantage of the fact that the chunks are ordered.

We can setup a background process that runs on regular intervals and performs the following steps:

1. Wait for a new commitlog chunk to be written out and then take note of the index of the chunk.
2. Flush all in-memory buffered data as compressed chunks to FDB (note that the storage engine will still be accepting writes while this operation is going on, but that’s fine, this flow only needs to ensure that all writes that were already acknowledged **before** the commitlog chunk from #1 was written out are flushed to FDB).
3. Delete all commitlog chunks with an index **lower** than the chunk from step #1. Note this operation is now safe because the previous step (if it succeeds) guarantees that all the data in all the commitlog chunks that will be deleted have already been persisted to FDB in the form of compressed data chunks.

[](./resources/fdb_time.png)

Using the diagram above as an example, the persistence loop would wait for chunk #3 to be flushed, then the buffer would begin flushing everything that was currently in-memory, and finally once that completed the storage engine could delete all commitlog chunks lower than 4 because all the data they contained was flushed to FDB as compressed chunks.

In code, it looks like this:

```golang
func (l *rawBlock) startPersistLoop() {
for {
  // Prevent excessive activity when there are no incoming writes.
  time.Sleep(persistLoopInterval)

  // truncToken is opaque to the caller but the commit log can use it
  // to truncate all chunks whose index is lower than the chunk that
  // was just flushed as part of the commit log rotation.
  truncToken, err := l.cl.WaitForRotation()
  if err != nil {
    log.Printf("error waiting for commitlog rotation: %v", err)
    continue
  }

  if err := l.buffer.Flush(); err != nil {
    log.Printf("error flushing buffer: %v", err)
    continue
  }

  if err := l.cl.Truncate(truncToken); err != nil {
    log.Printf("error truncating commitlog: %v", err)
    continue
  }
}
}
```

The last thing to consider about the commit log chunks is that once an instance is restarted it will need to read all of the existing chunks before accepting any writes or reads. I didn’t implement this in the prototype to save time and because I’m fairly certain it wouldn’t be an issue from a performance perspective because read performance is one of FDB’s strengths.

The next thing we need to understand is how the `buffer` works, both in terms of read and write operations, as well as the `Flush()` mechanism that we alluded to in the snippet above.

The `buffer` system's job is straightforward: buffer writes in-memory (actively Gorilla compressing them to save memory) until the compressed block can be merged with an existing one in FDB, or inserted as a new chunk entirely.

I won't go over the implementation of the encoders themselves because that's mainly just straightforward bit-fiddling and described well in the [Gorilla paper]([Gorilla](https://www.vldb.org/pvldb/vol8/p1816-teller.pdf). Also, the compression code is mostly just a knock-off of M3DB's ;). If you're really curious, you can check out the code for the [encoder](https://github.com/richardartoul/tsdb-layer/blob/master/src/encoding/encoder.go) and [decoder](https://github.com/richardartoul/tsdb-layer/blob/master/src/encoding/decoder.go) here. The only thing you really need to understand about the encoder and decoders are their (simplified) interfaces:

```golang
type Encoder interface {
 Encode(timestamp time.Time, value float64) error
 Bytes() []byte
}

type Decoder interface {
 Next() bool
 Current() (time.Time, float64)
 Err() error
 Reset(b []byte)
}
```

The implementation of the buffer itself is reasonably straightforward. The actual struct looks like this:

```golang
type buffer struct {
 sync.Mutex
 encoders map[string][]encoding.Encoder
}
```

The basic data structure is a synchronized hashmap from time series ID to an array of encoders. The existing implementation is simplified to make things easier and as a result has a few basic limitations (like the inability to write data points out of order) that would require a slightly more complicated data structure to solve, but the basic idea and performance would remain the same.

Let's start by looking at the write path. This is the most straightforward part. All the encoders are treated as immutable (except the last one), so writing is as simple as finding the last encoder for a given seriesID (or creating one if necessary), and then encoding the newest value into it.

```golang
func (b *buffer) Write(writes []layer.Write) error {
 b.Lock()
 defer b.Unlock()

 for _, w := range writes {
   encoders, ok := b.encoders[w.ID]
   if !ok {
     encoders = []encoding.Encoder{encoding.NewEncoder()}
     b.encoders[w.ID] = encoders
   }

   enc := encoders[len(encoders)-1]
   lastT, _, hasWrittenAnyValues := enc.LastEncoded()
   if hasWrittenAnyValues {
     if w.Timestamp.Before(lastT) {
       return fmt.Errorf(
         "cannot write data out of order, series: %s, prevTimestamp: %s, currTimestamp: %s",
         w.ID, lastT.String(), w.Timestamp.String())
     }
     if w.Timestamp.Equal(lastT) {
       return fmt.Errorf(
         "cannot upsert existing values, series: %s, currTimestamp: %s",
         w.ID, lastT.String())
     }
   }

   if err := enc.Encode(w.Timestamp, w.Value); err != nil {
     return err
   }
 }

 return nil
}
```

Before we discuss the Read path, we need to go over the `Flush` path which is how data gets moved from temporary storage in the in-memory buffers to persistent storage in FDB. Remember from our earlier discussion of the background "persist loop" that the contract of the `Flush` method is that when it completes all writes that were already in the buffer when the function started **must** be persisted to FDB.

The actual implementation (which you can read through [here](https://github.com/richardartoul/tsdb-layer/blob/master/src/layer/rawblock/buffer.go)) is unfortunately complicated by some complex synchronization and concurrency code that I don't want to delve into right now (mainly for performance reasons) but the basic idea is simple: iterate through every time series that was in memory when the function started, create a new encoder for it (making all previous encoders immutable) into which new writes will be encoded, and then flush all the immutable encoders to FDB.

The first step for flushing the in-memory encoder data to FDB is to retrieve the existing metadata for that series from FDB. The metadata stored in FDB for each series looks like this:

```golang
type tsMetadata struct {
 Chunks []chunkMetadata
}
```

and each `chunkMetadata` looks like this:

```golang
type chunkMetadata struct {
 Key       []byte
 First     time.Time
 Last      time.Time
 SizeBytes int
}
```

The series metadata entry serves as a sort of index for the series data by keeping track of all the compressed data chunks associated with that series. For each chunk it keeps track of:

1. The FDB key for that chunk (so that the chunk can be retrieved).
2. The timestamp for the first and last datapoint stored in the compressed block. This information is important for the read path as it informs us which chunks need to be retrieved to satisfy a query with a given time range. It can also be useful to the merge logic so that it can make better decisions about which chunks to merge together to form larger contiguous blocks.
3. The size (in bytes) of the chunk. This is used by the merging logic to determine when an encoder that is being flushed should be inserted as a new chunk or merged with an existing one.

This is where FDB's unique programming model really shines. I said earlier that FoundationDB provides the abstraction of a sorted key/value storage system, but more importantly, it supports completely ACID transactions at the highest level of isolation `strict serializability` (which means you're not vulnerable to the types of bugs described in [this excellent blog post by the FaunaDB team](https://fauna.com/blog/demystifying-database-systems-correctness-anomalies-under-serializable-isolation)).

Because of these guarantees, programming the flush logic is **almost** as simple as if we were programming against an in-memory system. In a single strict serializability ACID transaction we can do the following:

1. Read the existing metadata for the series being flushed.
2. Use the series metadata to decide if the data being flushed should be merged with an existing chunk or written out as a new, independent chunk (this makes experimenting with different compaction methods trivial since we don’t have to rewrite the underlying storage engine).
3. Read the existing chunk that we need to merge with (if necessary).
4. Write the merged (or new) chunk to FDB.
5. Write back the series metadata with the updated chunk information.

Everything we’ve accomplished up until this point could probably have been accomplished on any distributed system with a sorted key/value interface (of which there are many). However, implementing the 5 steps described above 100% correctly with no edge-cases or race conditions using a loosely / eventually consistent distributed system like Cassandra would be a nightmare. Accomplishing it with FDB is a breeze.

Finally, now that we've covered both the write and flush paths, we can discuss the read path. Implementing reads turns out to actually be quite straight forward. The steps are:

1. Read the latest version of the series metadata out of FDB.
2. Use the metadata to determine which chunk need to be pulled out of FDB to satisfy the provided query range (I.E if data points between the times of 12p.m and 2p.m are requested then any chunks where the `First`/`Last` data points intersect that range need to be pulled back).
3. Determine which in-memory encoders (which may not yet be flushed to FDB) also contain data points within the requested time range.
4. Return a decoder that will transparently iterate through all of the data points (returning them in order) by merging across all of the chunks retrieved from FDB as well as the in-memory encoders. This problem turns out to be equivalent to merging k sorted arrays and [this blog post](https://medium.com/outco/how-to-merge-k-sorted-arrays-c35d87aa298e) has a good explanation of how to accomplish that using a min heap. You can also take a look at my implementation [here](https://github.com/richardartoul/tsdb-layer/blob/master/src/encoding/multi_decoder.go).

A lot of effort went into optimizing the write path, but we haven't done much of anything to optimize the read path. The reason for that is two-fold:

1. Systems like M3DB are designed for workloads where write throughput is much higher than read throughput.
2. FDB can perform reads at a much higher rate than writes by default, so less optimization is required.

Let's pause for a moment and see if we’ve accomplished our goals. To reiterate, we wanted our system to implement the `Write` and `Read` interfaces (check) as well as satisfy the following properties:

1. Horizontal scalability - Check. Benchmarking shows that this design has a transaction conflict rate near zero which means the number of transactions we can do [should scale linearly as we add hardware](https://apple.github.io/foundationdb/performance.html).
2. High write throughput - Check. This implementation can easily handle over a million logical writes/s on my 2018 MacbookPro.
3. Efficient compression - Check. We’re using almost the exact same time series compression that M3DB and all the other popular time series databases use.

## Future Considerations and Extensions

DiamondDB is missing a ton of features, but most notably it lacks:

1. The ability to store and compress custom types
2. Secondary indexing (Ex. Fetch all time series where `city` tag equals `san_francisco`)
3. Automatic TTL (time to live I.E data should “expire” after a certain period of time)

### Complex Types

Storing and compressing custom types turns out to be the easiest to solve. All we have to do is replace our Gorilla encoder with one that can efficiently compress more complicated types. Fortunately, we had to solve that exact problem recently in M3DB as part of our plan to evolve it from a metrics store to a more general purpose time series database. The solution we came up with was to model our complex types as Protobufs and then write a general purpose compression scheme that can perform streaming delta compression of Protobuf messages much like Gorilla performs streaming delta compression of floats. The code for that solution is [open source](https://github.com/m3db/m3/tree/master/src/dbnode/encoding/proto) and could be lifted directly into DiamondDB. If you’re curious about how the bit-fiddly details of how the compression works, take a look at [this documentation](https://github.com/m3db/m3/blob/master/src/dbnode/encoding/proto/docs/encoding.md).

### Inverted / Secondary Indexing

Next up is secondary indexing. We already got a brief glimpse about how to perform secondary indexing in FDB earlier with the `flush` code where we atomically wrote a new time series chunk and updated the series’ metadata entry (which is effectively a secondary index over the compressed data chunks). Implementing exact-match secondary indexing would be fairly straightforward. For example, let's say we wanted to implement a tag-based inverted index like the one M3DB supports. For each unique combination of key/value pair in the index we would store an FDB entry that contained a list of all the time series IDs that were tagged with that unique combination. The image below depicts a simple example of how to store and index two separate time series:

[](./resources/fdb_index.png)

If we wanted to query for all the time series where the `city` tag is equal to `san_francisco` then we would retrieve the FDB entry with the key `<storage,index,term,city,san_francisco>` which would immediately tell us that there applicable series are `sf_num_widgets` and `sf_num_people`. More complicated queries could be executed by unioning and intersecting the results of these individual term queries. For example, it's not difficult to imagine how this simple schema could evaluate the query: “fetch all time series where `city` equals `san_francisco` OR `type` equals `widgets`. Ta da! We’ve just implemented a [postings list] on top of FDB.

Of course, this is a fairly naive solution that could end up using a lot of disk space. If we’re willing to exchange complexity for better compression, we could assign each time series a unique ID (an operation that can be implemented efficiently in FDB) and then store a list of integers instead of time series IDs. This would reduce the size of secondary index entries substantially, but we could take it even further by storing a [roaring bitmap](https://roaringbitmap.org/) instead of a list of integers.

Supporting regular expression queries (as M3DB does) gets more complicated, and if I’m being completely honest, I’d have to spend a few weeks building prototypes to come up with the best way to do this. Luckily, this is the internet, so I can just tell you my opinion with absolutely no evidence to back it up.

First, the naive solution. In addition to the index entries from the previous example, we could also store entries where the key is the tag name and the value is all unique values that exist for that tag. We could then retrieve those index entries on-demand and run regular expressions on them in memory. This would tell us which unique tag/value pairs exist that match the regular expression which we could then use to pull back the index entries from the previous example and look up the matching series IDs. For many use-cases this would be reasonably performant, but you didn’t put up with my rambling for this long to settle for anything reasonable!

M3DB handles regular expression queries by maintaining a [finite state transducer](https://en.wikipedia.org/wiki/Finite-state_transducer)(FST) for each tag in the inverted index (in our example above there would be an FST for the `city` tag and another for the `type` tag). The FST itself stores a mapping between all the unique values for the tag (`widgets` and `num_people` for the `type` tag) and an integer. In M3DB the integer is an offset into a file where a [roaring bitmap](https://roaringbitmap.org/) is stored that contains the unique integer IDs of all the time series that contain that tag. Andrew Gallant’s now infamous [blog post](https://blog.burntsushi.net/transducers/) is a great resource to understand why FSTs solve this problem so effectively, but the short of it is that they’re incredibly efficient in this situation because they have the dual properties of:

1. Compressing extremely well.
2. Supporting running regular expressions against them in a performant way.

Could we leverage this solution in our FDB-backed system? Possibly. We’ve already discussed storing complex data structures like roaring bitmaps in FDB and there is no reason we couldn’t do something similar with FSTs. One limitation we might run into, however, is the fact that an individual value in FDB can’t exceed 100KiB in size which seems like a show-stopper, but we could probably work around it. For example, it’s not hard to imagine designing an mmap-like interface in the programming language of your choice that provides the abstraction of a byte array of arbitrary size that is transparently split and mapped onto FDB. You could then fork / modify existing FST libraries to execute against this interface since many of them are already designed with the ability to execute against FSTs stored in byte arrays or mmap’d files.

### Data Time To Live (TTL)

Finally, let's talk about automatic TTLs (data expiry). I saved this one for last because it’s just a special case of secondary indexing and there are numerous ways you could build indices that would allow you to expire and clean up data in an efficient manner, but this blog post is already far too long.

## Conclusion

There are lots of distributed systems problems that are difficult to solve, but that can be implemented trivially as stateless layers over FoundationDB. However, some problems that seem like a poor match for FDB at first glance can actually be solved with a semi-stateful layer. Of course, building a semi-stateful layer is significantly more complicated than building a stateless one, but it's also significantly **less** complicated than building a distributed system from scratch. While I cut a lot of corners in my implementation, I was still able to build a distributed system that can accept millions of time series writes per second (with competitive levels of compression) in under 2000 lines of Go code. It's not hard to imagine that with a few couple weeks/months of dedicated work and a few thousand more lines of code that we could build this out into a production-ready system.

FoundationDB will never be able to beat a purpose designed storage engine, but programming against it is much easier than programming against the operating system, filesystem, network, and physical hardware. In a lot of ways building a distributed system on-top of FDB after having built one from scratch feels a lot like upgrading to Python from assembly.

FoundationDB is fast, reliable, easy to use, and a lot of fun to program against. Next time you need to build a distributed system consider if FDB could make your job a little bit easier. You might be surprised by just how far you can push it with the right design.


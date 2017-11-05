# chihaya-privtrak-middleware
A private tracker middleware for the [chihaya] BitTorrent tracker.

```bash
go get -t -u github.com/mrd0ll4r/chihaya-privtrak-middleware
```

[chihaya]: https://github.com/chihaya/chihaya

## Features
- In-memory storage of peer stats
- Versatile user ID handling (anything that fits into 16 bytes works)
- Generates deltas for user/infohash pairs
- Batches deltas, hands them off to a `DeltaHandler`, for further processing
- Purges stale peer stats
- Built for concurrency

## How to use this
**the whole thing is in early stages, beware**

Roughly, you need to do something like this:

1. Implement a `UserIdentifier` to derive user IDs from requests.
    You could use HTTP/UDP optional parameters for this, or customized routes, or ...
2. Implement a `DeltaHandler` to handle the deltas.
    You'd probably write these to a DB, or reprocess them first.
3. Compile this middleware into your chihaya instance by adding a few lines in the `cmd/chihay/config.go` file.
4. Adjust your config accordingly.

## Expected issues

- Should roughly double the memory usage of a tracker running [optmem].
    It'd be elegant to merge the two somehow, but I don't see that happening soon.
- Must run as a PreHook to throttle tracker throughput in case of a slow `DeltaHandler`.
    Otherwise we just get a goroutine explosion.

[optmem]: https://github.com/mrd0ll4r/chihaya-optmem-storage

## Future plans

- Add an interface to deal with backup/restore functionality
    Basically, it needs to:

    1. Restore everything on startup
    2. Be notified whenever we remove a peer due to GC (maybe also stopped peers?)
    3. Be asked if we know a user/infohash combination before we create it from scratch

- Possibly include IP/PeerID in the deltas?
    The `DeltaHandler` can figure out whether to remove these due to special user's protection.

## License
MIT, see [LICENSE](LICENSE)

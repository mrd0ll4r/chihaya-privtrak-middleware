# chihaya-privtrak-middleware
[![Build Status](https://api.travis-ci.org/mrd0ll4r/chihaya-privtrak-middleware.svg?branch=master)](https://travis-ci.org/mrd0ll4r/chihaya-privtrak-middleware)
[![Go Report Card](https://goreportcard.com/badge/github.com/mrd0ll4r/chihaya-privtrak-middleware)](https://goreportcard.com/report/github.com/mrd0ll4r/chihaya-privtrak-middleware)
[![GoDoc](https://godoc.org/github.com/mrd0ll4r/chihaya-privtrak-middleware?status.svg)](https://godoc.org/github.com/mrd0ll4r/chihaya-privtrak-middleware)
[![IRC Channel](https://img.shields.io/badge/freenode-%23chihaya-blue.svg "IRC Channel")](http://webchat.freenode.net/?channels=chihaya)

A private tracker middleware for the [chihaya] BitTorrent tracker.

```bash
go get -t -u github.com/mrd0ll4r/chihaya-privtrak-middleware
```

[chihaya]: https://github.com/chihaya/chihaya

## Features
- In-memory storage of peer stats
- Versatile user ID handling (anything unique that fits into 16 bytes works)
- Generates deltas for user/infohash pairs
- Batches deltas, hands them off to a `DeltaHandler`, for further processing
- Purges stale peer stats
- Built for concurrency

## Non-Features
- Doesn't check whether a user is allowed to announce.
    Write your own middleware for that, don't try to build one monolithic private tracker middleware.
- Doesn't check whether an infohash/client/... is whitelisted.
    Same as above.

## How to use this
**the whole thing is in early stages, beware**

Roughly, you need to do something like this:

1. Implement a `UserIdentifier` to derive unique user IDs from requests.
    You could use HTTP/UDP optional parameters for this, or customized routes, or ...
    Just make sure the process is fast and reliable.
    **Do not throw database queries in here, you WILL kill your tracker and/or DB.**
    A sane way of doing this would be to issue 16-byte passkeys and just use them as user IDs.
2. Implement a `DeltaHandler` to handle the deltas.
    You'd probably write these to a DB, or reprocess them first.
    They come in batches for a reason - don't insert them into a DB one by one.
    Before throwing them into a DB, you might want to
      - look up a user's numeric ID
      - look up the torrents numeric ID
      - (aggregate, possibly)
      - push them into an event processing system, for cool continuous queries
      - produce prometheus stats?
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
    2. Be notified whenever we remove a peer due to GC (maybe also stopped peers? What about partial seeds? What about paused downloads?)
    3. Figure out if we know a user/infohash combination before we create it from scratch

- Possibly include IP/PeerID in the deltas?
    The `DeltaHandler` can figure out whether to remove these due to special user's protection.

## License
MIT, see [LICENSE](LICENSE)

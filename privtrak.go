package privtrak

import (
	"bytes"
	"context"
	"errors"
	"hash/fnv"
	"sort"
	"sync"
	"time"

	"github.com/chihaya/chihaya/bittorrent"
	"github.com/chihaya/chihaya/middleware"
	"github.com/chihaya/chihaya/pkg/log"
	"github.com/chihaya/chihaya/pkg/timecache"
	"github.com/prometheus/client_golang/prometheus"
)

func init() {
	// Register the metrics.
	prometheus.MustRegister(
		PromGCDurationMilliseconds,
		PromUsersCount)
}

var (
	// PromGCDurationMilliseconds is a histogram used by storage to record the
	// durations of execution time required for removing expired peers.
	PromGCDurationMilliseconds = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "chihaya_privtrak_gc_duration_milliseconds",
		Help:    "The time it takes to perform privtrak garbage collection",
		Buckets: prometheus.ExponentialBuckets(9.375, 2, 10),
	})

	// PromUsersCount is a gauge used to hold the current total amount of
	// unique swarms being tracked by a storage.
	PromUsersCount = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "chihaya_privtrak_users_count",
		Help: "The number of users tracked",
	})
)

// recordGCDuration records the duration of a GC sweep.
func recordGCDuration(duration time.Duration) {
	PromGCDurationMilliseconds.Observe(float64(duration.Nanoseconds()) / float64(time.Millisecond))
}

// ErrClosing is returned on an announce if the middleware is shut down.
var ErrClosing = errors.New("privtrak middleware closing")

// An ID uniquely identifies a user.
type ID [16]byte

// A UserIdentifier identifies an ID from a request.
type UserIdentifier interface {
	DeriveID(*bittorrent.AnnounceRequest) (ID, error)
}

// A StatDelta is a delta for a user's stats.
// It contains the infohash, the event the client provided with the announce,
// the seeding duration and the reported event time.
type StatDelta struct {
	User        ID
	InfoHash    bittorrent.InfoHash
	DeltaUp     int64
	DeltaDown   int64
	Event       bittorrent.Event
	Reported    time.Time
	SeedingTime int64
}

// LogFields implements log.Fielder for StatDeltas.
func (s StatDelta) LogFields() log.Fields {
	return log.Fields{
		"user":      s.User,
		"infoHash":  s.InfoHash,
		"deltaUp":   s.DeltaUp,
		"deltaDown": s.DeltaDown,
		"event":     s.Event,
		"reported":  s.Reported,
	}
}

// A DeltaHandler handles batches of stat-deltas.
type DeltaHandler interface {
	// HandleDeltas handles a batch of deltas.
	// This operation is assumed to be atomic:
	//   if no error is returned, the deltas are assumed to be persisted and
	//     thus deleted from this middleware
	//   if an error is returned, the deltas are kept in the middleware and the
	//     error will be returned on the announce route. We will attempt to
	//     flush the deltas again next time, or during routine garbage
	//     collection.
	HandleDeltas([]StatDelta) error
}

type userSwarmStats struct {
	infoHash   bittorrent.InfoHash
	uploaded   uint64
	downloaded uint64
	// lastUpdate stores the unix seconds timestamp of the last received update
	// for this peer/infohash combination
	lastUpdate  int64
	seedingTime int64
}

// userSwarmStatsCollection is a slice of userSwarmStats.
// We make it a type to implement sort.Interface on it.
type userSwarmStatsCollection []userSwarmStats

func (s userSwarmStatsCollection) Len() int {
	return len(s)
}

func (s userSwarmStatsCollection) Less(i, j int) bool {
	return bytes.Compare(s[i].infoHash[:], s[j].infoHash[:]) < 0
}

func (s userSwarmStatsCollection) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func statsSearchFunc(ih bittorrent.InfoHash, s userSwarmStatsCollection) func(i int) bool {
	return func(i int) bool {
		return bytes.Compare(s[i].infoHash[:], ih[:]) >= 0
	}
}

type userStats struct {
	// these are sorted by infoHash
	swarmStats userSwarmStatsCollection
}

func (u *userStats) update(req *bittorrent.AnnounceRequest) *StatDelta {
	// check if we have stats for the swarm
	i := sort.Search(len(u.swarmStats), statsSearchFunc(req.InfoHash, u.swarmStats))
	log.Debug("privtrak: calculating update", log.Fields{"i": i}, req)

	if i < len(u.swarmStats) && bytes.Equal(u.swarmStats[i].infoHash[:], req.InfoHash[:]) {
		// if yes: generate delta, update. if event==stopped, delete
		log.Debug("privtrak: found", log.Fields{"i": i}, req)
		var delta *StatDelta
		deltaUp := int64(req.Uploaded) - int64(u.swarmStats[i].uploaded)
		deltaDown := int64(req.Downloaded) - int64(u.swarmStats[i].downloaded)
		if deltaUp != 0 || deltaDown != 0 {
			// Only emit a delta if there was a delta...
			delta = &StatDelta{
				DeltaUp:     deltaUp,
				DeltaDown:   deltaDown,
				Event:       req.Event,
				Reported:    timecache.Now(),
				SeedingTime: u.swarmStats[i].seedingTime,
			}
			copy(delta.InfoHash[:], req.InfoHash[:])
			u.swarmStats[i].seedingTime = 0
		}

		if req.Event == bittorrent.Stopped {
			// The peer left the swarm - delete our records.
			if u.swarmStats[i].seedingTime != 0 {
				delta = &StatDelta{
					Event:       req.Event,
					Reported:    timecache.Now(),
					SeedingTime: u.swarmStats[i].seedingTime,
				}
				copy(delta.InfoHash[:], req.InfoHash[:])
				u.swarmStats[i].seedingTime = 0
			}
			u.swarmStats = append(u.swarmStats[:i], u.swarmStats[i+1:]...)

		} else {
			if req.Left == 0 {
				//only count seeding time for completed torrents
				u.swarmStats[i].seedingTime += timecache.NowUnix() - u.swarmStats[i].lastUpdate
			}
			u.swarmStats[i].downloaded = req.Downloaded
			u.swarmStats[i].uploaded = req.Uploaded
			u.swarmStats[i].lastUpdate = timecache.NowUnix()
		}
		return delta
	}

	// if no: insert
	log.Debug("privtrak: not found", log.Fields{"i": i}, req)
	u.swarmStats = append(u.swarmStats, userSwarmStats{})
	copy(u.swarmStats[i+1:], u.swarmStats[i:])
	u.swarmStats[i] = userSwarmStats{
		uploaded:   req.Uploaded,
		downloaded: req.Downloaded,
		lastUpdate: timecache.NowUnix(),
	}
	copy(u.swarmStats[i].infoHash[:], req.InfoHash[:])

	return nil
}

func (u *userStats) removeExpired(peerLifetime time.Duration, id ID) []StatDelta {
	cutoffTime := timecache.Now().Add(peerLifetime * -1)
	cutoff := cutoffTime.Unix()

	deltas := make([]StatDelta, 0)
	for i := range u.swarmStats {
		if u.swarmStats[i].lastUpdate < cutoff {
			if u.swarmStats[i].seedingTime != 0 {
				delta := StatDelta{
					Event:       bittorrent.None,
					Reported:    timecache.Now(),
					SeedingTime: u.swarmStats[i].seedingTime,
				}
				copy(delta.User[:], id[:])
				copy(delta.InfoHash[:], u.swarmStats[i].infoHash[:])
				deltas = append(deltas, delta)
			}

			u.swarmStats = append(u.swarmStats[:i], u.swarmStats[i+1:]...)
			i--
		}
	}

	return deltas
}

type statShard struct {
	users  map[ID]userStats
	deltas []StatDelta
	sync.RWMutex
}

type ptMiddleware struct {
	cfg    Config
	shards []*statShard

	userIdentifier UserIdentifier
	deltaHandler   DeltaHandler

	closing chan struct{}
	wg      sync.WaitGroup
}

func (m *ptMiddleware) Stop() <-chan error {
	toReturn := make(chan error)
	go func() {
		close(m.closing)
		m.wg.Wait()

		m.flushAllSeedTimes()
		for i, shard := range m.shards {
			if len(shard.deltas) > 0 {
				err := m.deltaHandler.HandleDeltas(shard.deltas)
				if err != nil {
					toReturn <- err
				}
			}
			m.shards[i] = &statShard{}
		}

		close(toReturn)
	}()
	return toReturn
}

// New constructs a new instance of the privtrak middleware.
func New(provided Config, identifier UserIdentifier, handler DeltaHandler) (middleware.Hook, error) {
	cfg := provided.validate()
	log.Info("privtrak: creating new hook", cfg)

	mw := ptMiddleware{
		cfg:            cfg,
		shards:         make([]*statShard, cfg.ShardCount),
		userIdentifier: identifier,
		deltaHandler:   handler,
		closing:        make(chan struct{}),
	}

	for i := 0; i < int(cfg.ShardCount); i++ {
		mw.shards[i] = &statShard{
			users: make(map[ID]userStats),
		}
	}

	// Start a goroutine for garbage collection.
	mw.wg.Add(1)
	go func() {
		defer mw.wg.Done()
		for {
			select {
			case <-mw.closing:
				return
			case <-time.After(cfg.GCInterval):
				cutoff := time.Now().Add(-cfg.PeerLifetime)
				log.Debug("privtrak: purging peers with no announces since", log.Fields{"cutoff": cutoff})
				before := time.Now()
				mw.collectGarbage()
				recordGCDuration(time.Since(before))
			}
		}
	}()

	// Start a goroutine for reporting statistics to Prometheus.
	mw.wg.Add(1)
	go func() {
		defer mw.wg.Done()
		t := time.NewTicker(cfg.PrometheusReportingInterval)
		for {
			select {
			case <-mw.closing:
				t.Stop()
				return
			case <-t.C:
				before := time.Now()
				mw.populateProm()
				log.Debug("privtrak: populateProm() finished", log.Fields{"timeTaken": time.Since(before)})
			}
		}
	}()

	// Start a goroutine for flushing seedtimes
	mw.wg.Add(1)
	go func() {
		defer mw.wg.Done()
		t := time.NewTicker(cfg.SeedtimeFlushInterval)
		for {
			select {
			case <-mw.closing:
				t.Stop()
				return
			case <-t.C:
				before := time.Now()
				mw.flushAllSeedTimes()
				log.Debug("privtrak: flushAllSeedTimes() finished", log.Fields{"timeTaken": time.Since(before)})
			}
		}
	}()

	return &mw, nil
}

func (m *ptMiddleware) collectGarbage() {
	for _, shard := range m.shards {
		shard.Lock()
		for id, stats := range shard.users {
			expiredDeltas := stats.removeExpired(m.cfg.PeerLifetime, id)
			shard.deltas = append(shard.deltas, expiredDeltas...)

			if len(stats.swarmStats) == 0 {
				delete(shard.users, id)
			} else {
				shard.users[id] = stats
			}
		}
		shard.Unlock()
	}
}

func (u *userStats) flushSeedTimes(id ID) []StatDelta {
	userDeltas := make([]StatDelta, 0)

	for i, stats := range u.swarmStats {
		if stats.seedingTime > 0 {
			delta := StatDelta{
				Reported:    timecache.Now(),
				SeedingTime: stats.seedingTime,
				Event: bittorrent.None,
			}
			copy(delta.InfoHash[:], stats.infoHash[:])
			copy(delta.User[:], id[:])
			u.swarmStats[i].seedingTime = 0

			userDeltas = append(userDeltas, delta)
		}
	}
	return userDeltas
}

func (m *ptMiddleware) flushAllSeedTimes() {
	for _, shard := range m.shards {
		shard.Lock()
		for id, stats := range shard.users {
			partialDeltas := stats.flushSeedTimes(id)
			shard.deltas = append(shard.deltas, partialDeltas...)
		}
		shard.Unlock()
	}
}

func (m *ptMiddleware) populateProm() {
	var userCount int64
	for _, shard := range m.shards {
		shard.RLock()
		userCount += int64(len(shard.users))
		shard.RUnlock()
	}

	PromUsersCount.Set(float64(userCount))
}

func (m *ptMiddleware) shardIndex(id ID) uint32 {
	h := fnv.New32a()
	h.Write(id[:])
	return h.Sum32() % uint32(len(m.shards))
}

func (m *ptMiddleware) HandleAnnounce(ctx context.Context, req *bittorrent.AnnounceRequest, resp *bittorrent.AnnounceResponse) (context.Context, error) {
	select {
	case <-m.closing:
		return ctx, ErrClosing
	default:
	}

	id, err := m.userIdentifier.DeriveID(req)
	if err != nil {
		return ctx, err
	}
	shard := m.shards[m.shardIndex(id)]
	shard.Lock()
	defer shard.Unlock()

	stats, ok := shard.users[id]
	if !ok {
		stats = userStats{
			swarmStats: make(userSwarmStatsCollection, 0, 1),
		}
	}

	delta := stats.update(req)
	if delta != nil {
		copy(delta.User[:], id[:])
		log.Debug("privtrak: generated announce delta", delta)
		shard.deltas = append(shard.deltas, *delta)
	}

	expiredDeltas := stats.removeExpired(m.cfg.PeerLifetime, id)
	shard.deltas = append(shard.deltas, expiredDeltas...)

	if len(stats.swarmStats) == 0 {
		delete(shard.users, id)
	} else {
		shard.users[id] = stats
	}

	// This checks for >= instead of == because flushing stats can fail. In that
	// case we must keep them and try again.
	if uint(len(shard.deltas)) >= m.cfg.BatchSize {
		// Flush stats.
		err = m.deltaHandler.HandleDeltas(shard.deltas)
		if err != nil {
			return ctx, err
		}

		shard.deltas = make([]StatDelta, 0)
	}

	return ctx, nil
}

func (m *ptMiddleware) HandleScrape(ctx context.Context, _ *bittorrent.ScrapeRequest, _ *bittorrent.ScrapeResponse) (context.Context, error) {
	return ctx, nil
}

package privtrak

import (
	"bytes"
	"context"
	"sort"
	"testing"
	"time"

	"github.com/chihaya/chihaya/bittorrent"
	"github.com/stretchr/testify/require"
)

type testDatum struct {
	input    []announce
	expected []StatDelta
}

type announce struct {
	req   bittorrent.AnnounceRequest
	delay time.Duration
}

var (
	ih1  = bittorrent.InfoHashFromString("aaaaaaaaaabbbbbbbbbb")
	pID1 = bittorrent.PeerIDFromString("aaaaaaaaaabbbbbbbbbb")
	id1  = ID{'a', 'a', 'a', 'a', 'a', 'a', 'a', 'a', 'a', 'a', 'b', 'b', 'b', 'b', 'b', 'b'}
	ih2  = bittorrent.InfoHashFromString("bbbbbbbbbbaaaaaaaaaa")
	pID2 = bittorrent.PeerIDFromString("bbbbbbbbbbaaaaaaaaaa")
	id2  = ID{'b', 'b', 'b', 'b', 'b', 'b', 'b', 'b', 'b', 'b', 'a', 'a', 'a', 'a', 'a', 'a'}
)

var testData = []testDatum{
	{
		input: []announce{
			{
				req: bittorrent.AnnounceRequest{
					InfoHash:   ih1,
					Uploaded:   0,
					Downloaded: 0,
					Event:      bittorrent.Started,
					Peer: bittorrent.Peer{
						ID: pID1,
					},
				},
			},
			{
				req: bittorrent.AnnounceRequest{
					InfoHash:   ih1,
					Uploaded:   0,
					Downloaded: 100,
					Peer: bittorrent.Peer{
						ID: pID1,
					},
				},
			},
		},
		expected: []StatDelta{
			{
				User:      id1,
				InfoHash:  ih1,
				DeltaUp:   0,
				DeltaDown: 100,
				Event:     bittorrent.None,
			},
		},
	},
	{
		input: []announce{
			{
				req: bittorrent.AnnounceRequest{
					InfoHash:   ih1,
					Uploaded:   1000,
					Downloaded: 0,
					Event:      bittorrent.Started,
					Peer: bittorrent.Peer{
						ID: pID1,
					},
				},
			},
			{
				req: bittorrent.AnnounceRequest{
					InfoHash:   ih1,
					Uploaded:   1200,
					Downloaded: 100,
					Event:      bittorrent.Stopped,
					Peer: bittorrent.Peer{
						ID: pID1,
					},
				},
			},
		},
		expected: []StatDelta{
			{
				User:      id1,
				InfoHash:  ih1,
				DeltaUp:   200,
				DeltaDown: 100,
				Event:     bittorrent.Stopped,
			},
		},
	},
	{
		input: []announce{
			{
				req: bittorrent.AnnounceRequest{
					InfoHash:   ih1,
					Uploaded:   1000,
					Downloaded: 0,
					Event:      bittorrent.Started,
					Peer: bittorrent.Peer{
						ID: pID1,
					},
				},
			},
			{
				req: bittorrent.AnnounceRequest{
					InfoHash:   ih1,
					Uploaded:   900,
					Downloaded: 900,
					Peer: bittorrent.Peer{
						ID: pID2,
					},
				},
			},
			{
				req: bittorrent.AnnounceRequest{
					InfoHash:   ih1,
					Uploaded:   1200,
					Downloaded: 100,
					Event:      bittorrent.Stopped,
					Peer: bittorrent.Peer{
						ID: pID1,
					},
				},
			},
			{
				req: bittorrent.AnnounceRequest{
					InfoHash:   ih1,
					Uploaded:   1200,
					Downloaded: 100,
					Peer: bittorrent.Peer{
						ID: pID2,
					},
				},
			},
		},
		expected: []StatDelta{
			{
				User:      id1,
				InfoHash:  ih1,
				DeltaUp:   200,
				DeltaDown: 100,
				Event:     bittorrent.Stopped,
			},
			{
				User:      id2,
				InfoHash:  ih1,
				DeltaUp:   300,
				DeltaDown: -800,
				Event:     bittorrent.None,
			},
		},
	},
}

var testConfig = Config{
	ShardCount:                  1024,
	GCInterval:                  10 * time.Minute,
	PrometheusReportingInterval: 10 * time.Minute,
	PeerLifetime:                30 * time.Minute,
	BatchSize:                   1024,
}

type identifier struct{}

func (identifier) DeriveID(req *bittorrent.AnnounceRequest) (ID, error) {
	id := ID{}
	copy(id[:], req.ID[:])
	return id, nil
}

type handler struct {
	to *[]StatDelta
}

func (h handler) HandleDeltas(deltas []StatDelta) error {
	*h.to = append(*h.to, deltas...)
	return nil
}

func collector(to *[]StatDelta) DeltaHandler {
	return handler{to}
}

func TestMiddleware(t *testing.T) {
	for _, datum := range testData {
		doTest(t, testConfig, datum)
	}
}

func doTest(t *testing.T, cfg Config, d testDatum) {
	results := make([]StatDelta, 0)
	m, err := New(cfg, identifier{}, collector(&results))
	require.Nil(t, err)
	for _, in := range d.input {
		_, err := m.HandleAnnounce(context.Background(), &in.req, &bittorrent.AnnounceResponse{})
		require.Nil(t, err)
	}

	errs := m.(*ptMiddleware).Stop()
	for err = range errs {
		require.Nil(t, err)
	}

	sort.Slice(d.expected, func(i, j int) bool {
		switch bytes.Compare(d.expected[i].User[:], d.expected[j].User[:]) {
		case 0:
			return bytes.Compare(d.expected[i].InfoHash[:], d.expected[j].InfoHash[:]) == -1
		case -1:
			return true
		case 1:
			return false
		default:
			panic("bytes.Compare")
		}
	})
	sort.Slice(results, func(i, j int) bool {
		switch bytes.Compare(results[i].User[:], results[j].User[:]) {
		case 0:
			return bytes.Compare(results[i].InfoHash[:], results[j].InfoHash[:]) == -1
		case -1:
			return true
		case 1:
			return false
		default:
			panic("bytes.Compare")
		}
	})

	require.Equal(t, len(d.expected), len(results))
	for i := range results {
		require.Equal(t, d.expected[i].User, results[i].User)
		require.Equal(t, d.expected[i].InfoHash, results[i].InfoHash)
		require.Equal(t, d.expected[i].Event, results[i].Event)
		require.Equal(t, d.expected[i].DeltaDown, results[i].DeltaDown)
		require.Equal(t, d.expected[i].DeltaUp, results[i].DeltaUp)
	}
}

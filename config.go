package privtrak

import (
	"time"

	"github.com/chihaya/chihaya/pkg/log"
)

// Default config constants.
const (
	defaultShardCount                  = 1024
	defaultPrometheusReportingInterval = time.Second * 1
	defaultGarbageCollectionInterval   = time.Minute * 3
	defaultPeerLifetime                = time.Minute * 30
	defaultBatchSize                   = 1024
)

// A Config holds the configuration for the privtrak middleware.
type Config struct {
	BatchSize                   uint          `yaml:"batch_size"`
	ShardCount                  uint          `yaml:"shard_count"`
	GCInterval                  time.Duration `yaml:"gc_interval"`
	PrometheusReportingInterval time.Duration `yaml:"prometheus_reporting_interval"`
	PeerLifetime                time.Duration `json:"peer_lifetime"`
}

// LogFields renders the current config as a set of Logrus fields.
func (cfg Config) LogFields() log.Fields {
	return log.Fields{
		"gcInterval":         cfg.GCInterval,
		"promReportInterval": cfg.PrometheusReportingInterval,
		"peerLifetime":       cfg.PeerLifetime,
		"shardCount":         cfg.ShardCount,
		"batchSize":          cfg.BatchSize,
	}
}

func (cfg Config) validate() Config {
	validcfg := cfg

	if cfg.ShardCount <= 0 {
		validcfg.ShardCount = defaultShardCount
		log.Warn("falling back to default configuration", log.Fields{
			"name":     "ShardCount",
			"provided": cfg.ShardCount,
			"default":  validcfg.ShardCount,
		})
	}

	if cfg.GCInterval <= 0 {
		validcfg.GCInterval = defaultGarbageCollectionInterval
		log.Warn("falling back to default configuration", log.Fields{
			"name":     "GCInterval",
			"provided": cfg.GCInterval,
			"default":  validcfg.GCInterval,
		})
	}

	if cfg.PrometheusReportingInterval <= 0 {
		validcfg.PrometheusReportingInterval = defaultPrometheusReportingInterval
		log.Warn("falling back to default configuration", log.Fields{
			"name":     "PrometheusReportingInterval",
			"provided": cfg.PrometheusReportingInterval,
			"default":  validcfg.PrometheusReportingInterval,
		})
	}

	if cfg.PeerLifetime <= 0 {
		validcfg.PeerLifetime = defaultPeerLifetime
		log.Warn("falling back to default configuration", log.Fields{
			"name":     "PeerLifetime",
			"provided": cfg.PeerLifetime,
			"default":  validcfg.PeerLifetime,
		})
	}

	if cfg.BatchSize <= 0 {
		validcfg.BatchSize = defaultBatchSize
		log.Warn("falling back to default configuration", log.Fields{
			"name":     "BatchSize",
			"provided": cfg.BatchSize,
			"default":  validcfg.BatchSize,
		})
	}

	return validcfg
}

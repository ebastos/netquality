package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	DeviceID string `yaml:"device_id"`
	Listen   string `yaml:"listen"`
	DataDir  string `yaml:"data_dir"`

	Schedule  ScheduleConfig  `yaml:"schedule"`
	Gateway   GatewayConfig   `yaml:"gateway"`
	DNS       DNSConfig       `yaml:"dns"`
	Targets   []TargetConfig  `yaml:"targets"`
	Threshold ThresholdsConfig `yaml:"thresholds"`
	State     StateConfig     `yaml:"state"`
	Baseline  BaselineConfig  `yaml:"baseline"`
	Retention RetentionConfig `yaml:"retention"`

	ICMP ICMPConfig `yaml:"icmp"`
}

type ScheduleConfig struct {
	Interval  Duration `yaml:"interval"`
	DNSEvery  int      `yaml:"dns_every"`
	HTTPEvery int      `yaml:"http_every"`
}

type GatewayConfig struct {
	Enabled bool   `yaml:"enabled"`
	Host    string `yaml:"host"`
}

type DNSConfig struct {
	QueryHost  string   `yaml:"query_host"`
	Timeout    Duration `yaml:"timeout"`
	ResolverIP string   `yaml:"resolver_ip"`
}

type TargetConfig struct {
	Name   string `yaml:"name"`
	URL    string `yaml:"url"`
	Host   string `yaml:"host"`
	Port   int    `yaml:"port"`
	Method string `yaml:"method"`
	Mode   string `yaml:"mode"`
}

type ThresholdsConfig struct {
	Gateway GatewayThresholds `yaml:"gateway"`
	DNS     DNSThresholds     `yaml:"dns"`
	Path    PathThresholds    `yaml:"path"`
}

type GatewayThresholds struct {
	LossPctDegraded   float64 `yaml:"loss_pct_degraded"`
	LossPctDown       float64 `yaml:"loss_pct_down"`
	LatencyMsDegraded float64 `yaml:"latency_ms_degraded"`
	LatencyMsDown     float64 `yaml:"latency_ms_down"`
}

type DNSThresholds struct {
	LatencyMsDegraded float64 `yaml:"latency_ms_degraded"`
	LatencyMsDown     float64 `yaml:"latency_ms_down"`
}

type PathThresholds struct {
	LatencyMsDegraded float64 `yaml:"latency_ms_degraded"`
	LatencyMsDown     float64 `yaml:"latency_ms_down"`
	FailCountDown     int     `yaml:"fail_count_down"`
}

type StateConfig struct {
	Debounce           Duration `yaml:"debounce"`
	ClearDegradedAfter Duration `yaml:"clear_degraded_after"`
}

type BaselineConfig struct {
	WarmupDays        int      `yaml:"warmup_days"`
	RecomputeInterval Duration `yaml:"recompute_interval"`
	AnomalyMultiplier float64  `yaml:"anomaly_multiplier"`
}

type RetentionConfig struct {
	RawDays    int `yaml:"raw_days"`
	RollupDays int `yaml:"rollup_days"`
}

type ICMPConfig struct {
	Count   int      `yaml:"count"`
	Timeout Duration `yaml:"timeout"`
}

// Duration wraps time.Duration for YAML unmarshaling.
type Duration time.Duration

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d = Duration(parsed)
	return nil
}

func (d Duration) Std() time.Duration {
	return time.Duration(d)
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	cfg.applyDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) applyDefaults() {
	if c.DeviceID == "" {
		c.DeviceID = "netquality-default"
	}
	if c.Listen == "" {
		c.Listen = "127.0.0.1:8080"
	}
	if c.DataDir == "" {
		c.DataDir = "/var/lib/netquality"
	}
	if c.Schedule.Interval == 0 {
		c.Schedule.Interval = Duration(30 * time.Second)
	}
	if c.Schedule.DNSEvery <= 0 {
		c.Schedule.DNSEvery = 1
	}
	if c.Schedule.HTTPEvery <= 0 {
		c.Schedule.HTTPEvery = 1
	}
	if c.DNS.QueryHost == "" {
		c.DNS.QueryHost = "google.com"
	}
	if c.DNS.Timeout == 0 {
		c.DNS.Timeout = Duration(5 * time.Second)
	}
	if c.State.Debounce == 0 {
		c.State.Debounce = Duration(2 * time.Minute)
	}
	if c.State.ClearDegradedAfter == 0 {
		c.State.ClearDegradedAfter = Duration(5 * time.Minute)
	}
	if c.Baseline.WarmupDays <= 0 {
		c.Baseline.WarmupDays = 14
	}
	if c.Baseline.RecomputeInterval == 0 {
		c.Baseline.RecomputeInterval = Duration(time.Hour)
	}
	if c.Baseline.AnomalyMultiplier <= 0 {
		c.Baseline.AnomalyMultiplier = 1.5
	}
	if c.Retention.RawDays <= 0 {
		c.Retention.RawDays = 7
	}
	if c.Retention.RollupDays <= 0 {
		c.Retention.RollupDays = 90
	}
	if c.ICMP.Count <= 0 {
		c.ICMP.Count = 10
	}
	if c.ICMP.Timeout == 0 {
		c.ICMP.Timeout = Duration(10 * time.Second)
	}
	// thresholds defaults
	if c.Threshold.Gateway.LossPctDegraded == 0 {
		c.Threshold.Gateway.LossPctDegraded = 2
	}
	if c.Threshold.Gateway.LossPctDown == 0 {
		c.Threshold.Gateway.LossPctDown = 10
	}
	if c.Threshold.Gateway.LatencyMsDegraded == 0 {
		c.Threshold.Gateway.LatencyMsDegraded = 50
	}
	if c.Threshold.Gateway.LatencyMsDown == 0 {
		c.Threshold.Gateway.LatencyMsDown = 200
	}
	if c.Threshold.DNS.LatencyMsDegraded == 0 {
		c.Threshold.DNS.LatencyMsDegraded = 100
	}
	if c.Threshold.DNS.LatencyMsDown == 0 {
		c.Threshold.DNS.LatencyMsDown = 500
	}
	if c.Threshold.Path.LatencyMsDegraded == 0 {
		c.Threshold.Path.LatencyMsDegraded = 150
	}
	if c.Threshold.Path.LatencyMsDown == 0 {
		c.Threshold.Path.LatencyMsDown = 400
	}
	if c.Threshold.Path.FailCountDown <= 0 {
		c.Threshold.Path.FailCountDown = 3
	}
}

func (c *Config) validate() error {
	if c.Listen == "" {
		return fmt.Errorf("listen is required")
	}
	if c.Gateway.Enabled && c.Gateway.Host == "" {
		// auto-detect at runtime
	}
	for i, t := range c.Targets {
		if t.Name == "" {
			return fmt.Errorf("targets[%d]: name is required", i)
		}
		if t.URL == "" && t.Host == "" {
			return fmt.Errorf("targets[%d]: url or host is required", i)
		}
	}
	return nil
}

func (c *Config) DBPath() string {
	return c.DataDir + "/netquality.db"
}

package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the root configuration loaded from YAML (or defaults).
type Config struct {
	DeviceID string `yaml:"device_id"`
	Listen   string `yaml:"listen"`
	DataDir  string `yaml:"data_dir"`

	Schedule  ScheduleConfig   `yaml:"schedule"`
	Gateway   GatewayConfig    `yaml:"gateway"`
	DNS       DNSConfig        `yaml:"dns"`
	Targets   []TargetConfig   `yaml:"targets"`
	Threshold ThresholdsConfig `yaml:"thresholds"`
	State     StateConfig      `yaml:"state"`
	Baseline  BaselineConfig   `yaml:"baseline"`
	Retention RetentionConfig  `yaml:"retention"`

	ICMP ICMPConfig `yaml:"icmp"`
}

type ScheduleConfig struct {
	Interval  Duration `yaml:"interval"`
	DNSEvery  int      `yaml:"dns_every"`
	HTTPEvery int      `yaml:"http_every"`
}

type GatewayConfig struct {
	Enabled bool `yaml:"enabled"`
	// Host is optional. When empty and Enabled is true, the gateway is auto-detected
	// from the default route at runtime (see probe.NewRunner).
	Host string `yaml:"host"`
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

// Duration is a YAML-friendly wrapper around time.Duration (parses "1m30s" etc.).
type Duration time.Duration

// UnmarshalYAML implements the yaml.Unmarshaler interface.
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

// Std returns the underlying time.Duration (used when passing to timers, contexts, etc.).
func (d Duration) Std() time.Duration {
	return time.Duration(d)
}

// Load reads, parses, applies defaults, and validates the YAML config at the given path.
func Load(path string) (*Config, error) {
	// #nosec G304 - CWE-22 false positive. The path argument originates exclusively
	// from the -config CLI flag or NETQUALITY_CONFIG environment variable. Both are
	// controlled by the system administrator at daemon startup (typically via
	// systemd unit or manual invocation). There is no code path that accepts a
	// file path from network input, API clients, or any untrusted source.
	// Using os.Root would incorrectly constrain legitimate admin-specified
	// locations (e.g. /etc/netquality/config.yaml or a custom data_dir).
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

// setDefault assigns def to *p if *p is the zero value for T.
func setDefault[T comparable](p *T, def T) {
	var zero T
	if *p == zero {
		*p = def
	}
}

// setDefaultPositive assigns def to *p if *p <= 0.
// Used for counts, days, and threshold values that must be positive.
func setDefaultPositive[T ~int | ~float64](p *T, def T) {
	if *p <= 0 {
		*p = def
	}
}

// setDefaultDuration assigns def to *p if *p is zero.
// Separate helper because Duration is a defined type (not ~time.Duration).
func setDefaultDuration(p *Duration, def Duration) {
	if *p == 0 {
		*p = def
	}
}

func (c *Config) applyDefaults() {
	c.applyTopLevelDefaults()
	c.applyScheduleDefaults()
	c.applyDNSDefaults()
	c.applyStateDefaults()
	c.applyBaselineDefaults()
	c.applyRetentionDefaults()
	c.applyICMPDefaults()
	c.applyThresholdDefaults()
}

func (c *Config) applyTopLevelDefaults() {
	setDefault(&c.DeviceID, "netquality-default")
	setDefault(&c.Listen, "127.0.0.1:8080")
	setDefault(&c.DataDir, "/var/lib/netquality")
}

func (c *Config) applyScheduleDefaults() {
	setDefaultDuration(&c.Schedule.Interval, Duration(30*time.Second))
	setDefaultPositive(&c.Schedule.DNSEvery, 1)
	setDefaultPositive(&c.Schedule.HTTPEvery, 1)
}

func (c *Config) applyDNSDefaults() {
	setDefault(&c.DNS.QueryHost, "google.com")
	setDefaultDuration(&c.DNS.Timeout, Duration(5*time.Second))
}

func (c *Config) applyStateDefaults() {
	setDefaultDuration(&c.State.Debounce, Duration(2*time.Minute))
	setDefaultDuration(&c.State.ClearDegradedAfter, Duration(5*time.Minute))
}

func (c *Config) applyBaselineDefaults() {
	setDefaultPositive(&c.Baseline.WarmupDays, 14)
	setDefaultDuration(&c.Baseline.RecomputeInterval, Duration(time.Hour))
	setDefaultPositive(&c.Baseline.AnomalyMultiplier, 1.5)
}

func (c *Config) applyRetentionDefaults() {
	setDefaultPositive(&c.Retention.RawDays, 7)
	setDefaultPositive(&c.Retention.RollupDays, 90)
}

func (c *Config) applyICMPDefaults() {
	setDefaultPositive(&c.ICMP.Count, 10)
	setDefaultDuration(&c.ICMP.Timeout, Duration(10*time.Second))
}

func (c *Config) applyThresholdDefaults() {
	setDefaultPositive(&c.Threshold.Gateway.LossPctDegraded, 2)
	setDefaultPositive(&c.Threshold.Gateway.LossPctDown, 10)
	setDefaultPositive(&c.Threshold.Gateway.LatencyMsDegraded, 50)
	setDefaultPositive(&c.Threshold.Gateway.LatencyMsDown, 200)
	setDefaultPositive(&c.Threshold.DNS.LatencyMsDegraded, 100)
	setDefaultPositive(&c.Threshold.DNS.LatencyMsDown, 500)
	setDefaultPositive(&c.Threshold.Path.LatencyMsDegraded, 150)
	setDefaultPositive(&c.Threshold.Path.LatencyMsDown, 400)
	setDefaultPositive(&c.Threshold.Path.FailCountDown, 3)
}

func (c *Config) validate() error {
	if c.Listen == "" {
		return fmt.Errorf("listen is required")
	}
	// Gateway.Host may be empty when Enabled=true; auto-detection happens in probe.NewRunner.
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

// DBPath returns the full path to the SQLite database inside DataDir.
func (c *Config) DBPath() string {
	return c.DataDir + "/netquality.db"
}

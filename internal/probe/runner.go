package probe

import (
	"context"
	"fmt"
	"sync"

	"github.com/ebastos/netquality/internal/config"
	"github.com/ebastos/netquality/internal/store"
)

type Runner struct {
	cfg         *config.Config
	gatewayHost string
	cycle       int
	mu          sync.Mutex
}

func NewRunner(cfg *config.Config) (*Runner, error) {
	r := &Runner{cfg: cfg}
	if cfg.Gateway.Enabled {
		host := cfg.Gateway.Host
		if host == "" {
			var err error
			host, err = DefaultGateway()
			if err != nil {
				return nil, fmt.Errorf("detect gateway: %w", err)
			}
		}
		r.gatewayHost = host
	}
	return r, nil
}

func (r *Runner) GatewayHost() string {
	return r.gatewayHost
}

func (r *Runner) RunCycle(ctx context.Context) ([]store.Sample, error) {
	r.mu.Lock()
	r.cycle++
	cycle := r.cycle
	r.mu.Unlock()

	ts := store.NowUnix()
	var samples []store.Sample
	var mu sync.Mutex
	var wg sync.WaitGroup
	add := func(s ...store.Sample) {
		mu.Lock()
		samples = append(samples, s...)
		mu.Unlock()
	}

	r.runGatewayProbe(ctx, ts, add, &wg)
	r.runDNSProbe(ctx, cycle, ts, add, &wg)
	r.runPathProbes(ctx, cycle, ts, add, &wg)

	wg.Wait()
	return samples, nil
}

func (r *Runner) runGatewayProbe(ctx context.Context, ts int64, add func(...store.Sample), wg *sync.WaitGroup) {
	if !r.cfg.Gateway.Enabled || r.gatewayHost == "" {
		return
	}
	wg.Go(func() {
		res, err := ICMPProbe(ctx, r.gatewayHost, r.cfg.ICMP.Count, r.cfg.ICMP.Timeout.Std())
		if err != nil {
			add(
				sample(ts, "gateway", "loss_pct", 100, nil),
				sample(ts, "gateway", "latency_ms", 0, nil),
				sample(ts, "gateway", "jitter_ms", 0, nil),
				sample(ts, "gateway", "ok", 0, nil),
			)
			return
		}
		ok := 1.0
		if !res.OK {
			ok = 0
		}
		add(
			sample(ts, "gateway", "loss_pct", res.LossPct, nil),
			sample(ts, "gateway", "latency_ms", res.LatencyMs, nil),
			sample(ts, "gateway", "jitter_ms", res.JitterMs, nil),
			sample(ts, "gateway", "ok", ok, nil),
		)
	})
}

func (r *Runner) runDNSProbe(ctx context.Context, cycle int, ts int64, add func(...store.Sample), wg *sync.WaitGroup) {
	if (cycle-1)%r.cfg.Schedule.DNSEvery != 0 {
		return
	}
	wg.Go(func() {
		res, err := DNSProbe(ctx, r.cfg.DNS.QueryHost, r.cfg.DNS.Timeout.Std())
		ok := 1.0
		if err != nil || !res.OK {
			ok = 0
		}
		add(
			sample(ts, "dns", "latency_ms", res.LatencyMs, map[string]string{"resolver": res.Resolver}),
			sample(ts, "dns", "ok", ok, nil),
		)
		if r.cfg.DNS.ResolverIP != "" {
			r.runDNSResolverProbe(ctx, ts, add, r.cfg.DNS.ResolverIP)
		}
	})
}

func (r *Runner) runDNSResolverProbe(ctx context.Context, ts int64, add func(...store.Sample), resolverIP string) {
	res, err := DNSProbeResolver(ctx, r.cfg.DNS.QueryHost, resolverIP, r.cfg.DNS.Timeout.Std())
	ok2 := 1.0
	if err != nil || !res.OK {
		ok2 = 0
	}
	add(
		sample(ts, "dns:"+resolverIP, "latency_ms", res.LatencyMs, map[string]string{"resolver": res.Resolver}),
		sample(ts, "dns:"+resolverIP, "ok", ok2, nil),
	)
}

func (r *Runner) runPathProbes(ctx context.Context, cycle int, ts int64, add func(...store.Sample), wg *sync.WaitGroup) {
	if (cycle-1)%r.cfg.Schedule.HTTPEvery != 0 {
		return
	}
	for _, t := range r.cfg.Targets {
		target := t
		wg.Go(func() {
			probeName := "path:" + target.Name
			res, err := ProbeTarget(ctx, target.Name, target.URL, target.Host, target.Port, target.Method, target.Mode, r.cfg.DNS.Timeout.Std())
			ok := 1.0
			if err != nil || !res.OK {
				ok = 0
			}
			latency := res.LatencyMs
			if err != nil && latency == 0 {
				latency = float64(r.cfg.DNS.Timeout.Std().Milliseconds())
			}
			add(
				sample(ts, probeName, "latency_ms", latency, nil),
				sample(ts, probeName, "ok", ok, nil),
			)
		})
	}
}

func sample(ts int64, probe, metric string, value float64, labels map[string]string) store.Sample {
	if labels == nil {
		labels = map[string]string{}
	}
	return store.Sample{Ts: ts, Probe: probe, Metric: metric, Value: value, Labels: labels}
}

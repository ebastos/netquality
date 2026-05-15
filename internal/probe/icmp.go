package probe

import (
	"context"
	"fmt"
	"math"
	"time"

	probing "github.com/prometheus-community/pro-bing"
)

// ICMPResult holds the outcome of a privileged ICMP echo (ping) probe to the gateway.
type ICMPResult struct {
	LossPct   float64
	LatencyMs float64
	JitterMs  float64
	OK        bool
}

// ICMPProbe sends count ICMP Echo Requests and returns aggregate loss/latency/jitter.
// Requires CAP_NET_RAW (or root) because it uses raw sockets via pro-bing.
func ICMPProbe(ctx context.Context, host string, count int, timeout time.Duration) (ICMPResult, error) {
	pinger, err := probing.NewPinger(host)
	if err != nil {
		return ICMPResult{}, err
	}
	pinger.Count = count
	pinger.Timeout = timeout
	pinger.SetPrivileged(true)

	var res ICMPResult
	done := make(chan struct{})
	pinger.OnRecv = func(pkt *probing.Packet) {
		_ = pkt
	}
	pinger.OnFinish = func(stats *probing.Statistics) {
		res.OK = stats.PacketsRecv > 0 || stats.PacketLoss < 100
		res.LossPct = stats.PacketLoss
		res.LatencyMs = float64(stats.AvgRtt.Milliseconds())
		res.JitterMs = computeJitter(stats.Rtts, stats.StdDevRtt)
		close(done)
	}

	if err := pinger.Run(); err != nil {
		return ICMPResult{}, fmt.Errorf("ping %s: %w", host, err)
	}

	select {
	case <-ctx.Done():
		pinger.Stop()
		return ICMPResult{}, ctx.Err()
	case <-done:
	}

	if !res.OK && res.LossPct >= 100 {
		res.LossPct = 100
	}
	return res, nil
}

// computeJitter returns the jitter (population standard deviation) of RTT samples in milliseconds.
// When multiple RTTs are available it computes the value from the samples directly;
// otherwise it falls back to the library-provided StdDevRtt.
func computeJitter(rtts []time.Duration, stdDev time.Duration) float64 {
	if len(rtts) > 1 {
		var sum, sumSq float64
		for _, rtt := range rtts {
			ms := float64(rtt.Milliseconds())
			sum += ms
			sumSq += ms * ms
		}
		n := float64(len(rtts))
		mean := sum / n
		variance := sumSq/n - mean*mean
		if variance < 0 {
			variance = 0
		}
		return math.Sqrt(variance)
	}
	if stdDev > 0 {
		return float64(stdDev.Milliseconds())
	}
	return 0
}

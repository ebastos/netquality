package probe

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/go-ping/ping"
)

type ICMPResult struct {
	LossPct   float64
	LatencyMs float64
	JitterMs  float64
	OK        bool
}

func ICMPProbe(ctx context.Context, host string, count int, timeout time.Duration) (ICMPResult, error) {
	pinger, err := ping.NewPinger(host)
	if err != nil {
		return ICMPResult{}, err
	}
	pinger.Count = count
	pinger.Timeout = timeout
	pinger.SetPrivileged(true)

	var res ICMPResult
	done := make(chan struct{})
	pinger.OnRecv = func(pkt *ping.Packet) {
		_ = pkt
	}
	pinger.OnFinish = func(stats *ping.Statistics) {
		res.OK = stats.PacketsRecv > 0 || stats.PacketLoss < 100
		res.LossPct = stats.PacketLoss
		res.LatencyMs = float64(stats.AvgRtt.Milliseconds())
		if len(stats.Rtts) > 1 {
			var sum, sumSq float64
			for _, rtt := range stats.Rtts {
				ms := float64(rtt.Milliseconds())
				sum += ms
				sumSq += ms * ms
			}
			n := float64(len(stats.Rtts))
			mean := sum / n
			variance := sumSq/n - mean*mean
			if variance < 0 {
				variance = 0
			}
			res.JitterMs = math.Sqrt(variance)
		} else if stats.StdDevRtt > 0 {
			res.JitterMs = float64(stats.StdDevRtt.Milliseconds())
		}
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

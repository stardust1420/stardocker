package src

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	containerTypes "github.com/docker/docker/api/types/container"
	"github.com/docker/go-sdk/client"
)

// Live CPU and memory for the containers screen. Each tick (while no fetch
// is in flight) every running container is sampled once with the one-shot
// stats endpoint, which answers immediately but carries no previous CPU
// reading; CPU % is therefore computed against the sample from the tick
// before, so it appears one tick after a container shows up.

// cpuSample is one stats reading, enough to compute CPU % from two of them.
type cpuSample struct {
	total, system uint64
	online        uint32
	mem, memLimit uint64
}

// containerStats is what the table shows for one container.
type containerStats struct {
	cpu      float64
	hasCPU   bool // false until two samples exist
	mem      uint64
	memLimit uint64
}

type statsMsg struct {
	tickID  int
	samples map[string]cpuSample
}

// statsConcurrency limits parallel stats requests to the daemon.
const statsConcurrency = 8

// fetchStatsCmd samples the given running containers in parallel. A
// container that fails (e.g. it stopped meanwhile) is just left out.
func fetchStatsCmd(dockerClient client.SDKClient, tickID int, ids []string) tea.Cmd {
	return func() tea.Msg {
		out := make(map[string]cpuSample, len(ids))
		var (
			mu  sync.Mutex
			wg  sync.WaitGroup
			sem = make(chan struct{}, statsConcurrency)
		)
		for _, id := range ids {
			wg.Add(1)
			sem <- struct{}{}
			go func(id string) {
				defer func() { <-sem; wg.Done() }()
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				s, err := sampleStats(ctx, dockerClient, id)
				if err != nil {
					return
				}
				mu.Lock()
				out[id] = s
				mu.Unlock()
			}(id)
		}
		wg.Wait()
		return statsMsg{tickID: tickID, samples: out}
	}
}

func sampleStats(ctx context.Context, dockerClient client.SDKClient, id string) (cpuSample, error) {
	r, err := dockerClient.ContainerStatsOneShot(ctx, id)
	if err != nil {
		return cpuSample{}, err
	}
	defer r.Body.Close()
	var st containerTypes.StatsResponse
	if err := json.NewDecoder(r.Body).Decode(&st); err != nil {
		return cpuSample{}, err
	}
	online := st.CPUStats.OnlineCPUs
	if online == 0 {
		online = uint32(len(st.CPUStats.CPUUsage.PercpuUsage))
	}
	return cpuSample{
		total:    st.CPUStats.CPUUsage.TotalUsage,
		system:   st.CPUStats.SystemUsage,
		online:   online,
		mem:      memoryUsage(st.MemoryStats),
		memLimit: st.MemoryStats.Limit,
	}, nil
}

// memoryUsage is the figure `docker stats` shows: usage minus the page
// cache that the kernel can reclaim (inactive_file on cgroup v2,
// total_inactive_file on v1).
func memoryUsage(m containerTypes.MemoryStats) uint64 {
	cache := m.Stats["total_inactive_file"]
	if v, ok := m.Stats["inactive_file"]; ok && cache == 0 {
		cache = v
	}
	if cache < m.Usage {
		return m.Usage - cache
	}
	return m.Usage
}

// cpuPercent is the CPU use between two samples as `docker stats` computes
// it: the container's share of the host's CPU time, times the CPU count, so
// 200% means two full cores.
func cpuPercent(prev, cur cpuSample) (float64, bool) {
	if prev.system == 0 || cur.system <= prev.system || cur.total < prev.total {
		return 0, false
	}
	cpuDelta := float64(cur.total - prev.total)
	sysDelta := float64(cur.system - prev.system)
	online := float64(cur.online)
	if online == 0 {
		online = 1
	}
	return cpuDelta / sysDelta * online * 100, true
}

// mergeStats turns new samples into the table's stats, using the previous
// samples for CPU %. Containers that weren't sampled are dropped.
func mergeStats(prev map[string]cpuSample, cur map[string]cpuSample) map[string]containerStats {
	out := make(map[string]containerStats, len(cur))
	for id, s := range cur {
		cs := containerStats{mem: s.mem, memLimit: s.memLimit}
		if p, ok := prev[id]; ok {
			cs.cpu, cs.hasCPU = cpuPercent(p, s)
		}
		out[id] = cs
	}
	return out
}

func formatCPU(s containerStats, ok bool) string {
	if !ok || !s.hasCPU {
		return ""
	}
	return fmt.Sprintf("%.1f%%", s.cpu)
}

func formatMem(s containerStats, ok bool) string {
	if !ok || s.mem == 0 {
		return ""
	}
	return humanBinarySize(s.mem)
}

// humanBinarySize formats bytes in binary units (MiB, GiB), as docker
// stats does for memory.
func humanBinarySize(b uint64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	v := float64(b)
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%dB", b)
	}
	return fmt.Sprintf("%.3g%s", v, units[i])
}

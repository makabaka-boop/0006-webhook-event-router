// Package metrics 提供 Prometheus 指标注册、增量采集与文本渲染。
// 指标在请求处理与转发路径中被真实调用，不引入第三方依赖。
package metrics

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Registry 是并发安全的指标收集器。
type Registry struct {
	mu       sync.Mutex
	counters map[string]int64
}

// NewRegistry 构造指标收集器。
func NewRegistry() *Registry {
	return &Registry{counters: map[string]int64{}}
}

// Inc 对单个带标签的计数器增加 delta。
func (r *Registry) Inc(name string, labels map[string]string, delta int64) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counters[metricKey(name, labels)] += delta
}

// CountEvent 记录一次事件接入的结果状态。
func (r *Registry) CountEvent(status string) {
	r.Inc("webhook_events_total", map[string]string{"status": status}, 1)
}

// CountDelivery 记录一次投递尝试的结果状态。
func (r *Registry) CountDelivery(status string) {
	r.Inc("webhook_deliveries_total", map[string]string{"status": status}, 1)
}

func metricKey(name string, labels map[string]string) string {
	if len(labels) == 0 {
		return name
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(name)
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(k)
		b.WriteString(`="`)
		b.WriteString(labels[k])
		b.WriteString(`"`)
	}
	b.WriteByte('}')
	return b.String()
}

// Handler 返回渲染 Prometheus 文本格式的 HTTP 处理器。
func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		r.mu.Lock()
		defer r.mu.Unlock()
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		keys := make([]string, 0, len(r.counters))
		for k := range r.counters {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var b strings.Builder
		for _, k := range keys {
			b.WriteString(k)
			b.WriteByte(' ')
			b.WriteString(strconv.FormatInt(r.counters[k], 10))
			b.WriteByte('\n')
		}
		w.Write([]byte(b.String()))
	})
}

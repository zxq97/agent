package metric

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

type Labels map[string]string

type Registry struct {
	mu     sync.Mutex
	values map[string]float64
}

func NewRegistry() *Registry {
	return &Registry{values: map[string]float64{}}
}

var Default = NewRegistry()

func Inc(name string, labels Labels, delta float64) {
	Default.Inc(name, labels, delta)
}

func Observe(name string, labels Labels, value float64) {
	Default.Observe(name, labels, value)
}

func Render() string {
	return Default.Render()
}

func (r *Registry) Inc(name string, labels Labels, delta float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.values[metricKey(name, labels)] += delta
}

func (r *Registry) Observe(name string, labels Labels, value float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.values[metricKey(name, labels)] = value
}

func (r *Registry) Render() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	keys := make([]string, 0, len(r.values))
	for k := range r.values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s %g\n", k, r.values[k])
	}
	return b.String()
}

func metricKey(name string, labels Labels) string {
	if len(labels) == 0 {
		return name
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf(`%s="%s"`, k, labels[k]))
	}
	return name + "{" + strings.Join(parts, ",") + "}"
}

package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/aymenkrifa/kmap/internal/kube"
)

// podItem is the shape kmap reads from one entry of `get pods -o json`.
type podItem struct {
	Metadata struct {
		Name              string            `json:"name"`
		Labels            map[string]string `json:"labels"`
		CreationTimestamp time.Time         `json:"creationTimestamp"`
	} `json:"metadata"`
	Spec struct {
		Containers []struct {
			Image string `json:"image"`
		} `json:"containers"`
	} `json:"spec"`
	Status struct {
		Phase                 string            `json:"phase"`
		ContainerStatuses     []containerStatus `json:"containerStatuses"`
		InitContainerStatuses []containerStatus `json:"initContainerStatuses"`
	} `json:"status"`
}

type containerStatus struct {
	Ready        bool `json:"ready"`
	RestartCount int  `json:"restartCount"`
	State        struct {
		Waiting *struct {
			Reason string `json:"reason"`
		} `json:"waiting"`
		Terminated *struct {
			Reason string `json:"reason"`
		} `json:"terminated"`
	} `json:"state"`
}

type podJSON struct {
	Items []podItem `json:"items"`
}

// nsData is everything kmap knows about one namespace beyond its pods. Fields
// stay nil unless a selected column asked for them.
type nsData struct {
	metrics map[string]podMetric
	deploys map[string]deployInfo
	ingress []ingressRoute
}

type podMetric struct{ cpu, mem string }

type deployInfo struct{ desired, ready int }

type ingressRoute struct {
	service string
	host    string
	url     string
}

// urlsFor returns the ingress routes serving any of these workloads. Routes are
// matched on the backing service name, which is the workload name in practice;
// the pod's own app label is tried too so a renamed service still resolves.
func (n *nsData) urlsFor(workloads []string, p *podItem) []ingressRoute {
	if n == nil || len(n.ingress) == 0 {
		return nil
	}
	want := map[string]bool{}
	for _, w := range workloads {
		want[w] = true
	}
	if p != nil {
		if app := p.Metadata.Labels["app"]; app != "" {
			want[app] = true
		}
	}
	var out []ingressRoute
	for _, r := range n.ingress {
		if want[r.service] {
			out = append(out, r)
		}
	}
	return out
}

// fetch loads the datasets the chosen columns need, one call each per namespace.
func fetchNSData(ctx context.Context, r kube.Runner, env envRunner, ns string, needs dataset) (*nsData, error) {
	d := &nsData{}
	if needs&needsMetrics != 0 {
		// metrics-server may be absent; a missing metric is a dash, not an error
		if out, err := env.capture(ctx, r, "top", "pods", "-n", ns, "--no-headers"); err == nil {
			d.metrics = parseTopPods(out)
		}
	}
	if needs&needsDeploys != 0 {
		out, err := env.capture(ctx, r, "get", "deploy", "-n", ns, "-o", "json")
		if err != nil {
			return nil, fmt.Errorf("listing deployments in %s: %w", ns, err)
		}
		d.deploys = parseDeploys(out)
	}
	if needs&needsIngress != 0 {
		out, err := env.capture(ctx, r, "get", "ingress", "-n", ns, "-o", "json")
		if err != nil {
			return nil, fmt.Errorf("listing ingresses in %s: %w", ns, err)
		}
		d.ingress = parseIngresses(out)
	}
	return d, nil
}

// envRunner binds an environment so callers do not rethread it through argv.
type envRunner struct{ argv func(args ...string) []string }

func (e envRunner) capture(ctx context.Context, r kube.Runner, args ...string) ([]byte, error) {
	var buf bytes.Buffer
	if err := r.Run(ctx, e.argv(args...), &buf, io.Discard); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// parseTopPods reads `kubectl top pods --no-headers`: NAME CPU MEM.
func parseTopPods(b []byte) map[string]podMetric {
	out := map[string]podMetric{}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) >= 3 {
			out[f[0]] = podMetric{cpu: f[1], mem: f[2]}
		}
	}
	return out
}

func parseDeploys(b []byte) map[string]deployInfo {
	var raw struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Spec struct {
				Replicas *int `json:"replicas"`
			} `json:"spec"`
			Status struct {
				ReadyReplicas int `json:"readyReplicas"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil
	}
	out := map[string]deployInfo{}
	for _, d := range raw.Items {
		desired := 1 // absent means 1, per the API's default
		if d.Spec.Replicas != nil {
			desired = *d.Spec.Replicas
		}
		out[d.Metadata.Name] = deployInfo{desired: desired, ready: d.Status.ReadyReplicas}
	}
	return out
}

func parseIngresses(b []byte) []ingressRoute {
	var raw struct {
		Items []struct {
			Spec struct {
				TLS   []struct{} `json:"tls"`
				Rules []struct {
					Host string `json:"host"`
					HTTP struct {
						Paths []struct {
							Path    string `json:"path"`
							Backend struct {
								Service struct {
									Name string `json:"name"`
								} `json:"service"`
							} `json:"backend"`
						} `json:"paths"`
					} `json:"http"`
				} `json:"rules"`
			} `json:"spec"`
		} `json:"items"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil
	}
	var out []ingressRoute
	for _, in := range raw.Items {
		scheme := "http"
		if len(in.Spec.TLS) > 0 {
			scheme = "https"
		}
		for _, r := range in.Spec.Rules {
			if r.Host == "" {
				continue
			}
			for _, p := range r.HTTP.Paths {
				path := p.Path
				if path == "" {
					path = "/"
				}
				out = append(out, ingressRoute{
					service: p.Backend.Service.Name,
					host:    r.Host,
					url:     scheme + "://" + r.Host + path,
				})
			}
		}
	}
	return out
}

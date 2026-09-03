package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/aymenkrifa/kmap/internal/registry"
	"github.com/aymenkrifa/kmap/internal/ui"
)

// dataset is the extra cluster data a column needs beyond the pod list. Each
// one costs one more call per namespace, so columns declare what they use and
// pods fetches only the union of what was actually asked for.
type dataset uint8

const (
	needsMetrics dataset = 1 << iota
	needsDeploys
	needsIngress
)

// cell is everything a column can render from. pod is nil when the alias
// matched nothing, which is the row that says why.
type cell struct {
	target   registry.Target
	pod      *podItem
	ready    int
	total    int
	restarts int
	ns       *nsData
}

// column is one renderable column. Adding one means adding an entry to the
// registry below: a header, what data it needs, and how to render a cell.
type column struct {
	header string
	needs  dataset
	render func(c cell) string
}

const dash = "—"

var columns = map[string]column{
	"alias": {header: "ALIAS", render: func(c cell) string {
		return c.target.Alias
	}},
	"workload": {header: "WORKLOAD", render: func(c cell) string {
		if c.pod != nil && len(c.target.Workloads) > 1 {
			// name the workload this row is actually about
			return c.pod.Metadata.Labels["app"]
		}
		return strings.Join(c.target.Workloads, ",")
	}},
	"pod": {header: "POD", render: func(c cell) string {
		if c.pod == nil {
			return dash
		}
		return c.pod.Metadata.Name
	}},
	"ready": {header: "READY", render: func(c cell) string {
		if c.pod == nil {
			return dash
		}
		return fmt.Sprintf("%d/%d", c.ready, c.total)
	}},
	"status": {header: "STATUS", needs: needsDeploys, render: func(c cell) string {
		if c.pod == nil {
			return absentStatus(c)
		}
		return colorPhase(c.pod.Status.Phase, c.ready, c.total)
	}},
	"reason": {header: "REASON", render: func(c cell) string {
		if c.pod == nil {
			return dash
		}
		return podReason(*c.pod)
	}},
	"restarts": {header: "RESTARTS", render: func(c cell) string {
		if c.pod == nil {
			return dash
		}
		return fmt.Sprint(c.restarts)
	}},
	"age": {header: "AGE", render: func(c cell) string {
		if c.pod == nil {
			return dash
		}
		return age(c.pod.Metadata.CreationTimestamp)
	}},
	"cpu": {header: "CPU", needs: needsMetrics, render: func(c cell) string {
		if c.pod == nil {
			return dash
		}
		if m, ok := c.ns.metrics[c.pod.Metadata.Name]; ok {
			return m.cpu
		}
		return dash
	}},
	"mem": {header: "MEM", needs: needsMetrics, render: func(c cell) string {
		if c.pod == nil {
			return dash
		}
		if m, ok := c.ns.metrics[c.pod.Metadata.Name]; ok {
			return m.mem
		}
		return dash
	}},
	"image": {header: "IMAGE", render: func(c cell) string {
		if c.pod == nil || len(c.pod.Spec.Containers) == 0 {
			return dash
		}
		img := c.pod.Spec.Containers[0].Image
		if i := strings.LastIndex(img, "/"); i >= 0 {
			img = img[i+1:] // the registry path is the same for everything here
		}
		return img
	}},
	"url": {header: "URL", needs: needsIngress, render: func(c cell) string {
		urls := c.ns.urlsFor(c.target.Workloads, c.pod)
		if len(urls) == 0 {
			return dash
		}
		out := ui.Link(urls[0].url, urls[0].host)
		if n := len(urls) - 1; n > 0 {
			out += ui.Gray + fmt.Sprintf(" +%d", n) + ui.Reset
		}
		return out
	}},
	"docs": {header: "DOCS", needs: needsIngress, render: func(c cell) string {
		u := docsURL(c)
		if u == "" {
			return dash
		}
		return ui.Link(u, "docs")
	}},
	"namespace": {header: "NAMESPACE", render: func(c cell) string {
		return c.target.Namespace
	}},
}

// DefaultColumns is what pods shows when neither config nor --columns says
// otherwise. reason earns its default slot because a pod stuck in an init
// container otherwise reads as a bare "Pending", and docs because the whole
// point of recording those paths is not having to remember them. docs costs one
// `get ingress` per namespace; url stays opt-in since it shows the same origin.
var DefaultColumns = []string{"alias", "workload", "ready", "status", "reason", "restarts", "age", "docs"}

// docsURL builds the documentation URL for a target: the service's ingress
// origin with its configured docs path hung off it. Empty when the alias has no
// docs path configured, or nothing serves it.
func docsURL(c cell) string {
	if c.target.Docs == "" {
		return ""
	}
	urls := c.ns.urlsFor(c.target.Workloads, c.pod)
	if len(urls) == 0 {
		return ""
	}
	return urls[0].base + c.target.Docs
}

// resolveColumns turns names into columns, reporting an unknown name with the
// full list rather than silently dropping it.
func resolveColumns(names []string) ([]column, dataset, error) {
	if len(names) == 0 {
		names = DefaultColumns
	}
	out := make([]column, 0, len(names))
	var needs dataset
	for _, n := range names {
		col, ok := columns[strings.ToLower(strings.TrimSpace(n))]
		if !ok {
			return nil, 0, fmt.Errorf("unknown column %q (have: %s)", n, strings.Join(columnNames(), ", "))
		}
		out = append(out, col)
		needs |= col.needs
	}
	return out, needs, nil
}

func columnNames() []string {
	names := make([]string, 0, len(columns))
	for n := range columns {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// absentStatus distinguishes a workload that is scaled to zero from one that
// was never deployed. Both used to render an identical grey "absent".
func absentStatus(c cell) string {
	if c.ns == nil || c.ns.deploys == nil {
		return ui.Gray + "absent" + ui.Reset
	}
	for _, w := range c.target.Workloads {
		d, ok := c.ns.deploys[w]
		if !ok {
			continue
		}
		if d.desired == 0 {
			return ui.Yellow + "scaled to 0" + ui.Reset
		}
		return ui.Red + "no pods" + ui.Reset // desired > 0 but nothing running
	}
	return ui.Gray + "not deployed" + ui.Reset
}

// podReason explains a pod that is not simply running: the container's waiting
// or terminated reason, or how far through its init containers it is.
func podReason(p podItem) string {
	for _, cs := range p.Status.InitContainerStatuses {
		if cs.State.Waiting != nil && cs.State.Waiting.Reason != "" {
			return ui.Yellow + "Init:" + cs.State.Waiting.Reason + ui.Reset
		}
	}
	if done, total := initProgress(p); total > 0 && done < total {
		return ui.Yellow + fmt.Sprintf("Init:%d/%d", done, total) + ui.Reset
	}
	for _, cs := range p.Status.ContainerStatuses {
		if w := cs.State.Waiting; w != nil && w.Reason != "" {
			return ui.Red + w.Reason + ui.Reset
		}
		if tm := cs.State.Terminated; tm != nil && tm.Reason != "" && tm.Reason != "Completed" {
			return ui.Red + tm.Reason + ui.Reset
		}
	}
	return dash
}

func initProgress(p podItem) (done, total int) {
	total = len(p.Status.InitContainerStatuses)
	for _, cs := range p.Status.InitContainerStatuses {
		if cs.State.Terminated != nil && cs.State.Terminated.Reason == "Completed" {
			done++
		}
	}
	return done, total
}

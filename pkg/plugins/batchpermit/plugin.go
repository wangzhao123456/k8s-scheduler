package batchpermit

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/scheduler/framework"
)

const (
	// Name is the plugin name used in the scheduler registry and configurations.
	Name = "BatchPermit"

	// GroupAnnotation is the annotation key used to identify a batch group.
	GroupAnnotation = "batch.scheduling.k8s.io/group"

	// MinAvailableAnnotation defines the minimum number of pods required to start the batch.
	MinAvailableAnnotation = "batch.scheduling.k8s.io/min-available"

	// defaultPermitTimeout is the default time to wait for a gang to become schedulable.
	defaultPermitTimeout = 10 * time.Minute
)

// ensure the plugin implements the required interfaces.
var _ framework.PermitPlugin = &Plugin{}
var _ framework.PostBindPlugin = &Plugin{}
var _ framework.UnreservePlugin = &Plugin{}

// New returns a new instance of the plugin.
func New(_ context.Context, handle framework.Handle, _ framework.PluginConfig) (framework.Plugin, error) {
	return &Plugin{
		handle:     handle,
		groupState: make(map[string]*state),
	}, nil
}

// Plugin coordinates batch scheduling by holding pods until enough peers in the same group are ready to start.
type Plugin struct {
	mu         sync.Mutex
	handle     framework.Handle
	groupState map[string]*state
}

// state tracks the waiting pods and the expected size of the batch.
type state struct {
	minAvailable int
	waiting      sets.Set[string] // pod UIDs currently waiting in Permit phase
	started      bool             // whether the gang has been released
}

// Name returns the plugin name.
func (p *Plugin) Name() string { return Name }

// Permit is invoked before binding a pod. It holds the pod if the batch has not reached the minimum size.
func (p *Plugin) Permit(ctx context.Context, pod *v1.Pod, nodeName string) (*framework.Status, time.Duration) {
	group, minAvailable, ok := getGroupInfo(pod)
	if !ok {
		return framework.NewStatus(framework.Success, "pod does not participate in batch scheduling"), 0
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	key := fmt.Sprintf("%s/%s", pod.Namespace, group)
	st, exists := p.groupState[key]
	if !exists {
		st = &state{
			minAvailable: minAvailable,
			waiting:      sets.New[string](),
		}
		p.groupState[key] = st
	}

	if st.minAvailable != minAvailable {
		// reconcile with the latest pod annotation to avoid drift.
		st.minAvailable = minAvailable
	}

	st.waiting.Insert(string(pod.UID))

	readyCount := len(st.waiting)

	if st.started || readyCount >= st.minAvailable {
		if !st.started {
			st.started = true
			p.releaseGroupLocked(key, st)
		}
		klog.V(2).InfoS("Releasing batch", "group", key, "waiting", readyCount)
		return framework.NewStatus(framework.Success, "batch size satisfied"), 0
	}

	klog.V(3).InfoS("Holding pod for batch", "pod", klog.KObj(pod), "group", key, "minAvailable", st.minAvailable, "current", readyCount)
	return framework.NewStatus(framework.Wait, fmt.Sprintf("waiting for %d more pods in group %s", st.minAvailable-readyCount, group)), defaultPermitTimeout
}

// PostBind cleans up internal state after the pod has been bound.
func (p *Plugin) PostBind(ctx context.Context, state *framework.CycleState, pod *v1.Pod, nodeName string) {
	p.cleanup(pod)
}

// Unreserve is invoked when a reserved pod is rejected. We clean up the state to avoid leaking entries.
func (p *Plugin) Unreserve(ctx context.Context, state *framework.CycleState, pod *v1.Pod, nodeName string) {
	p.cleanup(pod)
}

func (p *Plugin) cleanup(pod *v1.Pod) {
	group, _, ok := getGroupInfo(pod)
	if !ok {
		return
	}
	key := fmt.Sprintf("%s/%s", pod.Namespace, group)
	p.mu.Lock()
	defer p.mu.Unlock()

	st, exists := p.groupState[key]
	if !exists {
		return
	}

	st.waiting.Delete(string(pod.UID))
	if st.waiting.Len() == 0 {
		delete(p.groupState, key)
	}
}

// releaseGroupLocked releases all waiting pods that belong to the provided batch key.
// Caller must hold p.mu.
func (p *Plugin) releaseGroupLocked(groupKey string, st *state) {
	waitingPods := p.handle.WaitingPods()
	released := 0
	waitingPods.Iterate(func(wp framework.WaitingPod) {
		pod := wp.GetPod()
		if pod == nil {
			return
		}

		g, _, ok := getGroupInfo(pod)
		if !ok {
			return
		}
		key := fmt.Sprintf("%s/%s", pod.Namespace, g)
		if key != groupKey {
			return
		}
		if !st.waiting.Has(string(pod.UID)) {
			return
		}

		wp.Allow(p.Name())
		released++
	})

	klog.V(2).InfoS("Allowed waiting batch pods", "group", groupKey, "count", released)
}

// getGroupInfo extracts the batch group metadata from pod annotations.
func getGroupInfo(pod *v1.Pod) (group string, minAvailable int, ok bool) {
	group, ok = pod.Annotations[GroupAnnotation]
	if !ok || group == "" {
		return "", 0, false
	}

	minStr, ok := pod.Annotations[MinAvailableAnnotation]
	if !ok {
		return "", 0, false
	}

	value, err := strconv.Atoi(minStr)
	if err != nil || value <= 0 {
		klog.V(2).InfoS("Invalid minAvailable annotation; ignoring batch scheduling", "pod", klog.KObj(pod), "value", minStr)
		return "", 0, false
	}

	return group, value, true
}

// BuildConfig constructs the plugin config for use with the scheduler plugin registry.
func BuildConfig() framework.PluginFactory {
	return func(ctx context.Context, handle framework.Handle, cfg framework.PluginConfig) (framework.Plugin, error) {
		return New(ctx, handle, cfg)
	}
}

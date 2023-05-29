package business

import (
	"context"
	"io"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"time"

	constant "github.com/yabuchan/virtual-node-v2/pkg/const"

	"github.com/yabuchan/virtual-node-v2/pkg/persistence"

	"github.com/virtual-kubelet/node-cli/manager"
	"github.com/virtual-kubelet/virtual-kubelet/log"
	"github.com/virtual-kubelet/virtual-kubelet/node/api"
	"github.com/virtual-kubelet/virtual-kubelet/trace"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const CriSocketPath = "/run/containerd/containerd.sock"
const PodLogRoot = "/var/log/vk-cri/"
const PodVolRoot = "/run/vk-cri/volumes/"
const PodLogRootPerms = 0755
const PodVolRootPerms = 0755

// Provider implements the virtual-kubelet provider interface and manages pods in a CRI runtime
// NOTE: Provider is not inteded as an alternative to Kubelet, rather it's intended for testing and POC purposes
//
//	As such, it is far from functionally complete and never will be. It provides the minimum function necessary
type Provider struct {
	resourceManager    *manager.ResourceManager
	podLogRoot         string
	podVolRoot         string
	nodeName           string
	operatingSystem    string
	internalIP         string
	daemonEndpointPort int32
	notifyStatus       func(*v1.Pod)
	providerHelper     persistence.ProviderHelper
}

func NewProvider(nodeName, operatingSystem string, internalIP string, resourceManager *manager.ResourceManager, daemonEndpointPort int32) (*Provider, error) {
	providerHelper, err := persistence.NewProviderHelper(constant.QueueBaseName, constant.TableName, constant.DeviceId)
	if err != nil {
		return nil, err
	}
	provider := Provider{
		resourceManager:    resourceManager,
		podLogRoot:         PodLogRoot,
		podVolRoot:         PodVolRoot,
		nodeName:           nodeName,
		operatingSystem:    operatingSystem,
		internalIP:         internalIP,
		daemonEndpointPort: daemonEndpointPort,
		providerHelper:     providerHelper,
	}
	err = os.MkdirAll(provider.podLogRoot, PodLogRootPerms)
	if err != nil {
		return nil, err
	}
	err = os.MkdirAll(provider.podVolRoot, PodVolRootPerms)
	if err != nil {
		return nil, err
	}

	p := &provider
	return p, err
}

func (p *Provider) CreatePod(ctx context.Context, pod *v1.Pod) error {
	ctx, span := trace.StartSpan(ctx, "business.CreatePod")
	defer span.End()
	log.G(ctx).Debugf("receive CreatePod %q", pod.Name)
	ctx = span.WithFields(ctx, log.Fields{
		"pod": path.Join(pod.GetNamespace(), pod.GetName()),
	})
	//return nil
	err := p.providerHelper.CreatePod(ctx, pod)
	span.SetStatus(err)
	return err
}

func (p *Provider) UpdatePod(ctx context.Context, pod *v1.Pod) error {
	log.G(ctx).Debugf("receive UpdatePod %q", pod.Name)
	return nil
}

func (p *Provider) DeletePod(ctx context.Context, pod *v1.Pod) error {
	ctx, span := trace.StartSpan(ctx, "business.DeletePod")
	defer span.End()
	log.G(ctx).Debugf("receive DeletePod %q", pod.Name)
	ctx = span.WithFields(ctx, log.Fields{
		"pod": path.Join(pod.GetNamespace(), pod.GetName()),
	})
	//return nil
	err := p.providerHelper.DeletePod(ctx, pod)
	span.SetStatus(err)
	return err
}

func (p *Provider) GetPod(ctx context.Context, namespace, name string) (*v1.Pod, error) {
	ctx, span := trace.StartSpan(ctx, "business.GetPod")
	defer span.End()
	ctx = span.WithFields(ctx, log.Fields{
		"pod": path.Join(namespace, name),
	})
	//return nil, nil
	pod, err := p.providerHelper.GetPod(ctx, namespace, name)
	span.SetStatus(err)
	return pod, err
}

func (p *Provider) GetContainerLogs(ctx context.Context, namespace, podName, containerName string, opts api.ContainerLogOpts) (io.ReadCloser, error) {
	log.G(ctx).Debugf("receive GetContainerLogs %q", containerName)
	return ioutil.NopCloser(strings.NewReader("log is not implemented")), nil
}

func (p *Provider) RunInContainer(ctx context.Context, namespace, name, container string, cmd []string, attach api.AttachIO) error {
	log.G(ctx).Debugf("receive ExecInContainer %q\n", container)
	return nil
}

func (p *Provider) GetPodStatus(ctx context.Context, namespace, name string) (*v1.PodStatus, error) {
	ctx, span := trace.StartSpan(ctx, "business.GetPodStatus")
	defer span.End()
	ctx = span.WithFields(ctx, log.Fields{
		"pod": path.Join(namespace, name),
	})
	//return nil, nil
	pod, err := p.providerHelper.GetPodStatus(ctx, namespace, name)
	span.SetStatus(err)
	return pod, err
}

func (p *Provider) GetPods(ctx context.Context) ([]*v1.Pod, error) {
	log.G(ctx).Debugf("receive GetPods")
	ctx, span := trace.StartSpan(ctx, "business.GetPods")
	defer span.End()
	//return nil, nil
	pods, err := p.providerHelper.GetPods(ctx)
	span.SetStatus(err)
	return pods, err
}

func (p *Provider) ConfigureNode(ctx context.Context, n *v1.Node) {
	n.Status.Capacity = p.capacity(ctx)
	n.Status.Conditions = p.nodeConditions()
	n.Status.Addresses = p.nodeAddresses()
	n.Status.DaemonEndpoints = p.nodeDaemonEndpoints()
	n.Status.NodeInfo.OperatingSystem = p.operatingSystem
}

func (p *Provider) capacity(ctx context.Context) v1.ResourceList {

	var cpuQ resource.Quantity
	cpuQ.Set(int64(100000))
	var memQ resource.Quantity
	memQ.Set(int64(100000000000))

	return v1.ResourceList{
		"cpu":    cpuQ,
		"memory": memQ,
		"pods":   resource.MustParse("1000"),
	}
}

func (p *Provider) nodeConditions() []v1.NodeCondition {
	// TODO: Make this configurable
	return []v1.NodeCondition{
		{
			Type:               "Ready",
			Status:             v1.ConditionTrue,
			LastHeartbeatTime:  metav1.Now(),
			LastTransitionTime: metav1.Now(),
			Reason:             "KubeletReady",
			Message:            "kubelet is ready.",
		},
		{
			Type:               "OutOfDisk",
			Status:             v1.ConditionFalse,
			LastHeartbeatTime:  metav1.Now(),
			LastTransitionTime: metav1.Now(),
			Reason:             "KubeletHasSufficientDisk",
			Message:            "kubelet has sufficient disk space available",
		},
		{
			Type:               "MemoryPressure",
			Status:             v1.ConditionFalse,
			LastHeartbeatTime:  metav1.Now(),
			LastTransitionTime: metav1.Now(),
			Reason:             "KubeletHasSufficientMemory",
			Message:            "kubelet has sufficient memory available",
		},
		{
			Type:               "DiskPressure",
			Status:             v1.ConditionFalse,
			LastHeartbeatTime:  metav1.Now(),
			LastTransitionTime: metav1.Now(),
			Reason:             "KubeletHasNoDiskPressure",
			Message:            "kubelet has no disk pressure",
		},
		{
			Type:               "NetworkUnavailable",
			Status:             v1.ConditionFalse,
			LastHeartbeatTime:  metav1.Now(),
			LastTransitionTime: metav1.Now(),
			Reason:             "RouteCreated",
			Message:            "RouteController created a route",
		},
	}

}

// Provider function to return a list of node addresses
func (p *Provider) nodeAddresses() []v1.NodeAddress {
	return []v1.NodeAddress{
		{
			Type:    "InternalIP",
			Address: p.internalIP,
		},
	}
}

// Provider function to return the daemon endpoint
func (p *Provider) nodeDaemonEndpoints() v1.NodeDaemonEndpoints {
	return v1.NodeDaemonEndpoints{
		KubeletEndpoint: v1.DaemonEndpoint{
			Port: p.daemonEndpointPort,
		},
	}
}

func (p *Provider) NotifyPods(ctx context.Context, f func(*v1.Pod)) {
	p.notifyStatus = f
	go p.statusLoop(ctx)
}

func (p *Provider) statusLoop(ctx context.Context) {
	t := time.NewTimer(5 * time.Second)
	if !t.Stop() {
		<-t.C
	}

	for {
		t.Reset(5 * time.Second)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}

		if err := p.notifyPodStatuses(ctx); err != nil {
			log.G(ctx).WithError(err).Error("Error updating node statuses")
		}
	}
}

func (p *Provider) notifyPodStatuses(ctx context.Context) error {
	ls, err := p.GetPods(ctx)
	if err != nil {
		return err
	}

	for _, pod := range ls {
		p.notifyStatus(pod)
	}

	return nil
}

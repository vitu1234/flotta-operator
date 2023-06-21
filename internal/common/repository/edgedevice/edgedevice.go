package edgedevice

import (
	"context"
	"errors"
	"reflect"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/project-flotta/flotta-operator/api/v1alpha1"
	"github.com/project-flotta/flotta-operator/internal/common/indexer"
	"github.com/project-flotta/flotta-operator/internal/common/labels"
)

//go:generate mockgen -package=edgedevice -destination=mock_edgedevice.go . Repository
type Repository interface {
	Read(ctx context.Context, name string, namespace string) (*v1alpha1.EdgeDevice, error)
	ReadForPlaybookExecution(ctx context.Context, naplaybookExecutionNameme string, namespace string) (*v1alpha1.EdgeDevice, error)
	Create(ctx context.Context, edgeDevice *v1alpha1.EdgeDevice) error
	PatchStatus(ctx context.Context, edgeDevice *v1alpha1.EdgeDevice, patch *client.Patch) error
	Patch(ctx context.Context, old, new *v1alpha1.EdgeDevice) error
	ListForSelector(ctx context.Context, selector *metav1.LabelSelector, namespace string) ([]v1alpha1.EdgeDevice, error)
	ListForWorkload(ctx context.Context, name string, namespace string) ([]v1alpha1.EdgeDevice, error)
	ListForEdgeConfig(ctx context.Context, name string, namespace string) ([]v1alpha1.EdgeDevice, error)
	RemoveFinalizer(ctx context.Context, edgeDevice *v1alpha1.EdgeDevice, finalizer string) error
	UpdateLabels(ctx context.Context, device *v1alpha1.EdgeDevice, labels map[string]string) error
}

type CRRepository struct {
	client client.Client
}

func NewEdgeDeviceRepository(client client.Client) *CRRepository {
	return &CRRepository{client: client}
}

func (r *CRRepository) Read(ctx context.Context, name string, namespace string) (*v1alpha1.EdgeDevice, error) {
	edgeDevice := v1alpha1.EdgeDevice{}
	err := r.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &edgeDevice)
	return &edgeDevice, err
}

func (r *CRRepository) ReadForPlaybookExecution(ctx context.Context, playbookExecutionName string, namespace string) (*v1alpha1.EdgeDevice, error) {
	options := client.ListOptions{
		Namespace: namespace,
	}
	var edl v1alpha1.EdgeDeviceList
	err := r.client.List(ctx, &edl, &options)
	if err != nil {
		return nil, err
	}
	for _, device := range edl.Items {
		if device.Status.PlaybookExecutions != nil {
			for _, pe := range device.Status.PlaybookExecutions {
				if pe.Name == playbookExecutionName {
					return &device, nil
				}
			}
		}
	}

	return nil, errors.New("cannot find edge device")
}

func (r *CRRepository) Create(ctx context.Context, edgeDevice *v1alpha1.EdgeDevice) error {
	return r.client.Create(ctx, edgeDevice)
}

func (r *CRRepository) PatchStatus(ctx context.Context, edgeDevice *v1alpha1.EdgeDevice, patch *client.Patch) error {
	return r.client.Status().Patch(ctx, edgeDevice, *patch)
}

func (r *CRRepository) Patch(ctx context.Context, old, new *v1alpha1.EdgeDevice) error {
	patch := client.MergeFrom(old)
	return r.client.Patch(ctx, new, patch)
}

func (r CRRepository) ListForSelector(ctx context.Context, selector *metav1.LabelSelector, namespace string) ([]v1alpha1.EdgeDevice, error) {
	s, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return nil, err
	}
	options := client.ListOptions{
		Namespace:     namespace,
		LabelSelector: s,
	}
	var edl v1alpha1.EdgeDeviceList
	err = r.client.List(ctx, &edl, &options)
	if err != nil {
		return nil, err
	}

	return edl.Items, nil
}

func (r CRRepository) ListForWorkload(ctx context.Context, name string, namespace string) ([]v1alpha1.EdgeDevice, error) {
	var edl v1alpha1.EdgeDeviceList
	err := r.client.List(ctx, &edl, client.MatchingLabels{indexer.DeviceByWorkloadIndexKey: name}, client.InNamespace(namespace))
	if err != nil {
		return nil, err
	}
	return edl.Items, nil
}

func (r CRRepository) ListForEdgeConfig(ctx context.Context, name string, namespace string) ([]v1alpha1.EdgeDevice, error) {
	var edl v1alpha1.EdgeDeviceList
	err := r.client.List(ctx, &edl, client.MatchingLabels{labels.ConfigLabelPrefix + indexer.DeviceByConfigIndexKey: name}, client.InNamespace(namespace))
	if err != nil {
		return nil, err
	}

	return edl.Items, nil
}

func (r *CRRepository) RemoveFinalizer(ctx context.Context, edgeDevice *v1alpha1.EdgeDevice, finalizer string) error {
	cp := edgeDevice.DeepCopy()

	var finalizers []string
	for _, f := range cp.Finalizers {
		if f != finalizer {
			finalizers = append(finalizers, f)
		}
	}
	cp.Finalizers = finalizers

	err := r.Patch(ctx, edgeDevice, cp)
	if err == nil {
		edgeDevice.Finalizers = cp.Finalizers
	}

	return nil
}

func (r *CRRepository) UpdateLabels(ctx context.Context, device *v1alpha1.EdgeDevice, labels map[string]string) error {
	err := r.updateLabels(ctx, device, labels)
	if err == nil {
		return nil
	}

	// retry patching the edge device labels because the device can be update concurrently
	for i := 1; i < 4; i++ {
		time.Sleep(time.Duration(i*50) * time.Millisecond)
		device2, err := r.Read(ctx, device.Name, device.Namespace)
		if err != nil {
			continue
		}
		err = r.updateLabels(ctx, device2, labels)
		if err == nil {
			return nil
		}
	}
	return err
}

func (r *CRRepository) updateLabels(ctx context.Context, device *v1alpha1.EdgeDevice, labels map[string]string) error {
	deviceCopy := device.DeepCopy()
	deviceLabels := deviceCopy.Labels
	if deviceLabels == nil {
		deviceLabels = make(map[string]string)
	}
	for key, value := range labels {
		deviceLabels[key] = value
	}
	if reflect.DeepEqual(deviceLabels, device.Labels) {
		return nil
	}
	deviceCopy.Labels = deviceLabels
	err := r.Patch(ctx, device, deviceCopy)
	if err == nil {
		device.Labels = deviceCopy.Labels
	}
	return err
}

package yggdrasil_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-openapi/runtime/middleware"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/project-flotta/flotta-operator/api/v1alpha1"
	"github.com/project-flotta/flotta-operator/internal/common/metrics"
	"github.com/project-flotta/flotta-operator/internal/common/repository/edgedevice"
	"github.com/project-flotta/flotta-operator/internal/common/repository/playbookexecution"
	"github.com/project-flotta/flotta-operator/internal/edgeapi/backend/k8s"
	"github.com/project-flotta/flotta-operator/internal/edgeapi/configmaps"
	"github.com/project-flotta/flotta-operator/internal/edgeapi/devicemetrics"
	"github.com/project-flotta/flotta-operator/internal/edgeapi/images"
	"github.com/project-flotta/flotta-operator/internal/edgeapi/yggdrasil"
	"github.com/project-flotta/flotta-operator/models"
	"github.com/project-flotta/flotta-operator/pkg/mtls"
	api "github.com/project-flotta/flotta-operator/restapi/operations/yggdrasil"
	operations "github.com/project-flotta/flotta-operator/restapi/operations/yggdrasil"
)

const (
	testNamespace = "test-ns"

	MessageTypeData string              = "data"
	AuthzKey        mtls.RequestAuthKey = "APIAuthzkey" // the same as yggdrasil one, but important if got changed
)

var _ = Describe("Yggdrasil", func() {
	var (
		mockCtrl             *gomock.Controller
		repositoryMock       *k8s.MockRepositoryFacade
		playbookExecRepoMock *playbookexecution.MockRepository
		edgeDeviceRepoMock   *edgedevice.MockRepository
		metricsMock          *metrics.MockMetrics
		registryAuth         *images.MockRegistryAuthAPI
		handler              *yggdrasil.Handler
		eventsRecorder       *record.FakeRecorder
		allowListsMock       *devicemetrics.MockAllowListGenerator
		configMap            *configmaps.MockConfigMap

		errorNotFound = errors.NewNotFound(schema.GroupResource{Group: "", Resource: "notfound"}, "notfound")
		boolTrue      = true

		k8sClient client.Client
		testEnv   *envtest.Environment

		logger, _ = zap.NewDevelopment()
	)

	initKubeConfig := func() {
		By("bootstrapping test environment")
		testEnv = &envtest.Environment{
			CRDDirectoryPaths: []string{
				filepath.Join("../../..", "config", "crd", "bases"),
				filepath.Join("../../..", "config", "test", "crd"),
			},
			ErrorIfCRDPathMissing: true,
		}
		var err error
		cfg, err := testEnv.Start()
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg).NotTo(BeNil())

		k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
		Expect(err).NotTo(HaveOccurred())

		nsSpec := corev1.Namespace{ObjectMeta: v1.ObjectMeta{Name: testNamespace}}
		err = k8sClient.Create(context.TODO(), &nsSpec)
		Expect(err).NotTo(HaveOccurred())
	}

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		repositoryMock = k8s.NewMockRepositoryFacade(mockCtrl)
		playbookExecRepoMock = playbookexecution.NewMockRepository(mockCtrl)
		edgeDeviceRepoMock = edgedevice.NewMockRepository(mockCtrl)
		metricsMock = metrics.NewMockMetrics(mockCtrl)
		registryAuth = images.NewMockRegistryAuthAPI(mockCtrl)
		eventsRecorder = record.NewFakeRecorder(1)
		allowListsMock = devicemetrics.NewMockAllowListGenerator(mockCtrl)
		configMap = configmaps.NewMockConfigMap(mockCtrl)
		assembler := k8s.NewConfigurationAssembler(
			allowListsMock,
			nil,
			configMap,
			eventsRecorder,
			registryAuth,
			repositoryMock,
		)
		backend := k8s.NewBackend(repositoryMock, assembler, logger.Sugar(), testNamespace, eventsRecorder)
		handler = yggdrasil.NewYggdrasilHandler(testNamespace, metricsMock, nil, logger.Sugar(), backend, edgeDeviceRepoMock, playbookExecRepoMock)
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	getDevice := func(name string) *v1alpha1.EdgeDevice {
		return &v1alpha1.EdgeDevice{
			TypeMeta:   v1.TypeMeta{},
			ObjectMeta: v1.ObjectMeta{Name: name, Namespace: testNamespace},
			Spec: v1alpha1.EdgeDeviceSpec{
				RequestTime: &v1.Time{},
				Heartbeat:   &v1alpha1.HeartbeatConfiguration{},
			},
		}
	}

	Context("GetControlMessageForDevice", func() {
		var (
			params = api.GetControlMessageForDeviceParams{
				DeviceID: "foo",
			}
			deviceCtx = context.WithValue(context.TODO(), AuthzKey, mtls.RequestAuthVal{CommonName: "foo"})
		)

		It("cannot retrieve another device", func() {
			// given
			ctx := context.WithValue(context.TODO(), AuthzKey, mtls.RequestAuthVal{CommonName: "bar"})
			metricsMock.EXPECT().IncEdgeDeviceInvalidOwnerCounter().Times(1)

			// when
			res := handler.GetControlMessageForDevice(ctx, params)

			// then
			Expect(res).To(Equal(operations.NewGetControlMessageForDeviceForbidden()))
		})

		It("cannot retrieve device with empty context", func() {
			// given
			metricsMock.EXPECT().IncEdgeDeviceInvalidOwnerCounter().Times(1)

			// when
			res := handler.GetControlMessageForDevice(context.TODO(), params)

			// then
			Expect(res).To(Equal(operations.NewGetControlMessageForDeviceForbidden()))
		})

		It("Can retrieve message correctly", func() {
			// given
			device := getDevice("foo")
			repositoryMock.EXPECT().
				GetEdgeDevice(gomock.Any(), "foo", testNamespace).
				Return(device, nil).
				Times(1)

			// when
			res := handler.GetControlMessageForDevice(deviceCtx, params)

			// then
			Expect(res).To(Equal(operations.NewGetControlMessageForDeviceOK()))
		})

		It("Device does not exists", func() {
			// given
			repositoryMock.EXPECT().
				GetEdgeDevice(gomock.Any(), "foo", testNamespace).
				Return(nil, errorNotFound).
				Times(1)

			metricsMock.EXPECT().
				IncEdgeDeviceUnregistration().
				Times(1)

			// when
			res := handler.GetControlMessageForDevice(deviceCtx, params)
			data := res.(*api.GetControlMessageForDeviceOK)

			// then
			Expect(data.Payload.Type).To(Equal("command"))
		})

		It("Cannot retrieve device", func() {
			// given
			repositoryMock.EXPECT().
				GetEdgeDevice(gomock.Any(), "foo", testNamespace).
				Return(nil, fmt.Errorf("Failed")).
				Times(1)

			// when
			res := handler.GetControlMessageForDevice(deviceCtx, params)

			// then
			Expect(res).To(Equal(operations.NewGetControlMessageForDeviceInternalServerError()))
		})

		It("EdgeDevice removal causes disconnect command to be sent", func() {
			// given
			device := getDevice("foo")
			device.DeletionTimestamp = &v1.Time{Time: time.Now()}

			repositoryMock.EXPECT().
				GetEdgeDevice(gomock.Any(), "foo", testNamespace).
				Return(device, nil).
				Times(1)

			metricsMock.EXPECT().
				IncEdgeDeviceUnregistration().
				Times(1)

			// when
			res := handler.GetControlMessageForDevice(deviceCtx, params)
			data := res.(*api.GetControlMessageForDeviceOK)

			// then
			Expect(data.Payload.Type).To(Equal("command"))
		})

		It("Finalizers don't prevent disconnect command from being sent", func() {
			// given
			device := getDevice("foo")
			device.DeletionTimestamp = &v1.Time{Time: time.Now()}
			device.Finalizers = []string{"yggdrasil-connection-finalizer"}
			repositoryMock.EXPECT().
				GetEdgeDevice(gomock.Any(), "foo", testNamespace).
				Return(device, nil).
				Times(1)

			metricsMock.EXPECT().
				IncEdgeDeviceUnregistration().
				Times(1)

			// when
			res := handler.GetControlMessageForDevice(deviceCtx, params)
			data := res.(*api.GetControlMessageForDeviceOK)

			// then
			Expect(data.Payload.Type).To(Equal("command"))
		})

	})

	Context("GetDataMessageForDevice", func() {

		var (
			params = api.GetDataMessageForDeviceParams{
				DeviceID: "foo",
			}
			deviceCtx = context.WithValue(context.TODO(), AuthzKey, mtls.RequestAuthVal{CommonName: "foo"})
		)

		validateAndGetDeviceConfig := func(res middleware.Responder) models.DeviceConfigurationMessage {

			data, ok := res.(*operations.GetDataMessageForDeviceOK)
			ExpectWithOffset(1, ok).To(BeTrue())
			ExpectWithOffset(1, data.Payload.Type).To(Equal(MessageTypeData))

			content, ok := data.Payload.Content.(models.DeviceConfigurationMessage)

			ExpectWithOffset(1, ok).To(BeTrue())
			return content
		}

		It("Trying to access with not owning device", func() {
			// given
			ctx := context.WithValue(context.TODO(), AuthzKey, mtls.RequestAuthVal{CommonName: "bar"})
			metricsMock.EXPECT().IncEdgeDeviceInvalidOwnerCounter().Times(1)

			// when
			res := handler.GetDataMessageForDevice(ctx, params)

			// then
			Expect(res).To(Equal(operations.NewGetDataMessageForDeviceForbidden()))
		})

		It("Trying to access no context device", func() {
			// given
			metricsMock.EXPECT().IncEdgeDeviceInvalidOwnerCounter().Times(1)
			// when
			res := handler.GetDataMessageForDevice(context.TODO(), params)

			// then
			Expect(res).To(Equal(operations.NewGetDataMessageForDeviceForbidden()))
		})

		It("Device is not in repo", func() {
			// given
			repositoryMock.EXPECT().
				GetEdgeDevice(gomock.Any(), "foo", testNamespace).
				Return(nil, errorNotFound).
				Times(1)

			// when
			res := handler.GetDataMessageForDevice(deviceCtx, params)

			// then
			Expect(res).To(Equal(operations.NewGetDataMessageForDeviceNotFound()))
		})

		It("Device repo failed", func() {
			// given
			repositoryMock.EXPECT().
				GetEdgeDevice(gomock.Any(), "foo", testNamespace).
				Return(nil, fmt.Errorf("failed")).
				Times(1)

			// when
			res := handler.GetDataMessageForDevice(deviceCtx, params)

			// then
			Expect(res).To(Equal(operations.NewGetDataMessageForDeviceInternalServerError()))
		})

		It("Delete without finalizer", func() {
			// given
			device := getDevice("foo")
			device.DeletionTimestamp = &v1.Time{Time: time.Now()}

			repositoryMock.EXPECT().
				GetEdgeDevice(gomock.Any(), "foo", testNamespace).
				Return(device, nil).
				Times(1)

			// when
			res := handler.GetDataMessageForDevice(deviceCtx, params)

			// then
			Expect(res).To(BeAssignableToTypeOf(&operations.GetDataMessageForDeviceOK{}))
			config := validateAndGetDeviceConfig(res)
			Expect(config.DeviceID).To(Equal("foo"))
			Expect(config.Workloads).To(HaveLen(0))
		})

		It("Delete with invalid finalizer", func() {
			// given
			device := getDevice("foo")
			device.DeletionTimestamp = &v1.Time{Time: time.Now()}
			device.Finalizers = []string{"foo"}

			repositoryMock.EXPECT().
				GetEdgeDevice(gomock.Any(), "foo", testNamespace).
				Return(device, nil).
				Times(1)

			// when
			res := handler.GetDataMessageForDevice(deviceCtx, params)

			// then
			Expect(res).To(BeAssignableToTypeOf(&operations.GetDataMessageForDeviceOK{}))
			config := validateAndGetDeviceConfig(res)
			Expect(config.DeviceID).To(Equal("foo"))
			Expect(config.Workloads).To(HaveLen(0))
		})

		It("Retrieval of workload failed", func() {
			// given
			device := getDevice("foo")
			device.Status.Workloads = []v1alpha1.Workload{{Name: "workload1"}}

			repositoryMock.EXPECT().
				GetEdgeDevice(gomock.Any(), "foo", testNamespace).
				Return(device, nil).
				Times(1)

			repositoryMock.EXPECT().
				GetEdgeWorkload(gomock.Any(), "workload1", testNamespace).
				Return(nil, fmt.Errorf("Failed"))

			// when
			res := handler.GetDataMessageForDevice(deviceCtx, params)

			// then
			Expect(res).To(Equal(operations.NewGetDataMessageForDeviceInternalServerError()))
		})

		It("Cannot find workload for device status", func() {
			// given
			deviceName := "foo"
			device := getDevice(deviceName)
			device.Status.Workloads = []v1alpha1.Workload{{Name: "workload1"}}

			repositoryMock.EXPECT().
				GetEdgeDevice(gomock.Any(), deviceName, testNamespace).
				Return(device, nil).
				Times(1)

			repositoryMock.EXPECT().
				GetEdgeWorkload(gomock.Any(), "workload1", testNamespace).
				Return(nil, errorNotFound)

			// when
			res := handler.GetDataMessageForDevice(deviceCtx, params)

			// then
			Expect(res).To(BeAssignableToTypeOf(&operations.GetDataMessageForDeviceOK{}))
			config := validateAndGetDeviceConfig(res)
			Expect(config.DeviceID).To(Equal(deviceName))
			Expect(config.Workloads).To(HaveLen(0))
		})

		It("Workload status reported correctly on device status", func() {
			// given
			deviceName := "foo"
			device := getDevice(deviceName)
			device.Status.Workloads = []v1alpha1.Workload{{Name: "workload1"}}

			repositoryMock.EXPECT().
				GetEdgeDevice(gomock.Any(), deviceName, testNamespace).
				Return(device, nil).
				Times(1)

			workloadData := &v1alpha1.EdgeWorkload{
				ObjectMeta: v1.ObjectMeta{
					Name:      "workload1",
					Namespace: "default",
				},
				Spec: v1alpha1.EdgeWorkloadSpec{
					DeviceSelector: &v1.LabelSelector{
						MatchLabels: map[string]string{"test": "test"},
					},
					Type: "pod",
					Pod:  v1alpha1.Pod{},
					Data: &v1alpha1.DataConfiguration{},
				}}

			configMap.EXPECT().Fetch(gomock.Any(), gomock.Any(), gomock.Any()).Return(models.ConfigmapList{}, nil)
			repositoryMock.EXPECT().
				GetEdgeWorkload(gomock.Any(), "workload1", testNamespace).
				Return(workloadData, nil)

			// when
			res := handler.GetDataMessageForDevice(deviceCtx, params)

			// then
			Expect(res).To(BeAssignableToTypeOf(&operations.GetDataMessageForDeviceOK{}))
			config := validateAndGetDeviceConfig(res)

			Expect(config.DeviceID).To(Equal(deviceName))
			Expect(config.Workloads).To(HaveLen(1))
			workload := config.Workloads[0]
			Expect(workload.Name).To(Equal("workload1"))
			Expect(workload.Namespace).To(Equal("default"))
			Expect(workload.ImageRegistries).To(BeNil())
		})

		Context("Logs", func() {

			var (
				deviceName string = "foo"
				deviceCtx         = context.WithValue(context.TODO(), AuthzKey, mtls.RequestAuthVal{CommonName: deviceName})
				device     *v1alpha1.EdgeDevice
				deploy     *v1alpha1.EdgeWorkload
			)

			getWorkload := func(name string, ns string) *v1alpha1.EdgeWorkload {
				return &v1alpha1.EdgeWorkload{
					ObjectMeta: v1.ObjectMeta{
						Name:      name,
						Namespace: ns,
					},
					Spec: v1alpha1.EdgeWorkloadSpec{
						Type: "pod",
						Pod:  v1alpha1.Pod{},
					},
				}
			}

			BeforeEach(func() {
				deviceName = "foo"
				device = getDevice(deviceName)
				device.Status.Workloads = []v1alpha1.Workload{{Name: "workload1"}}

				repositoryMock.EXPECT().
					GetEdgeDevice(gomock.Any(), deviceName, testNamespace).
					Return(device, nil).
					Times(1)

				deploy = getWorkload("workload1", testNamespace)
				repositoryMock.EXPECT().
					GetEdgeWorkload(gomock.Any(), "workload1", testNamespace).
					Return(deploy, nil)

				configMap.EXPECT().Fetch(gomock.Any(), gomock.Any(), gomock.Any()).Return(models.ConfigmapList{}, nil)
			})

			It("LogCollection is defined as expected", func() {

				// given
				repositoryMock.EXPECT().GetConfigMap(
					gomock.Any(), "syslog-config", testNamespace).
					Return(&corev1.ConfigMap{
						Data: map[string]string{
							"Address": "127.0.0.1:512",
						},
					}, nil).
					Times(1)

				device.Spec.LogCollection = map[string]*v1alpha1.LogCollectionConfig{
					"syslog": {
						Kind:         "syslog",
						BufferSize:   10,
						SyslogConfig: &v1alpha1.NameRef{Name: "syslog-config"},
					},
				}
				deploy.Spec.LogCollection = "syslog"
				// when
				res := handler.GetDataMessageForDevice(deviceCtx, params)

				// then
				Expect(res).To(BeAssignableToTypeOf(&operations.GetDataMessageForDeviceOK{}))
				config := validateAndGetDeviceConfig(res)

				Expect(config.DeviceID).To(Equal(deviceName))

				// Device Log config
				Expect(config.Configuration.LogCollection).To(HaveKey("syslog"))
				logConfig := config.Configuration.LogCollection["syslog"]
				Expect(logConfig.Kind).To(Equal("syslog"))
				Expect(logConfig.BufferSize).To(Equal(int32(10)))
				Expect(logConfig.SyslogConfig.Address).To(Equal("127.0.0.1:512"))
				Expect(logConfig.SyslogConfig.Protocol).To(Equal("tcp"))

				Expect(config.Workloads).To(HaveLen(1))
				workload := config.Workloads[0]
				Expect(workload.Name).To(Equal("workload1"))
				Expect(workload.LogCollection).To(Equal("syslog"))
				Expect(workload.ImageRegistries).To(BeNil())
			})

			It("CM with invalid protocol", func() {

				// given
				repositoryMock.EXPECT().GetConfigMap(gomock.Any(), "syslog-config", testNamespace).
					Return(&corev1.ConfigMap{
						Data: map[string]string{
							"Address":  "127.0.0.1:512",
							"Protocol": "invalid",
						},
					}, nil).
					Times(1)

				device.Spec.LogCollection = map[string]*v1alpha1.LogCollectionConfig{
					"syslog": {
						Kind:         "syslog",
						BufferSize:   10,
						SyslogConfig: &v1alpha1.NameRef{Name: "syslog-config"},
					},
				}
				deploy.Spec.LogCollection = "syslog"
				// when
				res := handler.GetDataMessageForDevice(deviceCtx, params)

				// then

				Expect(res).To(Equal(operations.NewGetDataMessageForDeviceInternalServerError()))
			})

			It("No valid cm", func() {

				// given
				repositoryMock.EXPECT().GetConfigMap(
					gomock.Any(), "syslog-config", testNamespace).
					Return(nil, fmt.Errorf("Invalid error")).
					Times(1)

				device.Spec.LogCollection = map[string]*v1alpha1.LogCollectionConfig{
					"syslog": {
						Kind:         "syslog",
						BufferSize:   10,
						SyslogConfig: &v1alpha1.NameRef{Name: "syslog-config"},
					},
				}
				deploy.Spec.LogCollection = "syslog"
				// when
				res := handler.GetDataMessageForDevice(deviceCtx, params)

				// then
				Expect(res).To(Equal(operations.NewGetDataMessageForDeviceInternalServerError()))
			})
		})

		Context("Metrics", func() {
			var (
				deviceName      string = "foo"
				deviceCtx       context.Context
				device          *v1alpha1.EdgeDevice
				allowList       = models.MetricsAllowList{Names: []string{"foo", "bar"}}
				allowListName   = "foo"
				caSecretName    = "test"
				caSecretKeyName = "ca.crt"
			)

			BeforeEach(func() {
				deviceName = "foo"
				deviceCtx = context.WithValue(context.TODO(), AuthzKey, mtls.RequestAuthVal{CommonName: "foo"})
				device = getDevice(deviceName)
				device.Status.Workloads = []v1alpha1.Workload{{Name: "workload1"}}

				repositoryMock.EXPECT().
					GetEdgeDevice(gomock.Any(), deviceName, testNamespace).
					Return(device, nil).
					Times(1)
			})

			getWorkload := func(name string, ns string) *v1alpha1.EdgeWorkload {
				return &v1alpha1.EdgeWorkload{
					ObjectMeta: v1.ObjectMeta{
						Name:      name,
						Namespace: ns,
					},
					Spec: v1alpha1.EdgeWorkloadSpec{
						Type: "pod",
						Pod:  v1alpha1.Pod{},
					},
				}
			}

			It("Path, port, interval, allowList is honored", func() {
				// given
				expectedResult := &models.Metrics{
					Path:      "/metrics",
					Port:      9999,
					Interval:  55,
					AllowList: &allowList,
				}

				deploy := getWorkload("workload1", testNamespace)
				deploy.Spec.Metrics = &v1alpha1.ContainerMetricsConfiguration{
					Path: "/metrics", Port: 9999, Interval: 55, AllowList: &v1alpha1.NameRef{
						Name: allowListName,
					}}

				configMap.EXPECT().Fetch(gomock.Any(), gomock.Any(), gomock.Any()).Return(models.ConfigmapList{}, nil)
				repositoryMock.EXPECT().
					GetEdgeWorkload(gomock.Any(), "workload1", testNamespace).
					Return(deploy, nil)

				allowListsMock.EXPECT().
					GenerateFromConfigMap(gomock.Any(), allowListName, testNamespace).
					Return(&allowList, nil).Times(1)

				// when
				res := handler.GetDataMessageForDevice(deviceCtx, params)

				// then
				Expect(res).To(BeAssignableToTypeOf(&operations.GetDataMessageForDeviceOK{}))
				config := validateAndGetDeviceConfig(res)

				Expect(config.DeviceID).To(Equal(deviceName))
				Expect(config.Workloads).To(HaveLen(1))
				Expect(config.Workloads[0].Metrics).To(Equal(expectedResult))
			})

			It("AllowList configmap retrival error", func() {

				// given
				deploy := getWorkload("workload1", testNamespace)
				deploy.Spec.Metrics = &v1alpha1.ContainerMetricsConfiguration{
					Path: "/metrics", Port: 9999, Interval: 55, AllowList: &v1alpha1.NameRef{
						Name: allowListName,
					}}

				repositoryMock.EXPECT().
					GetEdgeWorkload(gomock.Any(), "workload1", testNamespace).
					Return(deploy, nil)

				allowListsMock.EXPECT().
					GenerateFromConfigMap(gomock.Any(), allowListName, testNamespace).
					Return(nil, fmt.Errorf("Failed to get CM")).Times(1)

				// when
				res := handler.GetDataMessageForDevice(deviceCtx, params)

				// then
				Expect(res).To(BeAssignableToTypeOf(&operations.GetDataMessageForDeviceInternalServerError{}))
			})

			It("Path and port in containers is honored", func() {

				// given
				expectedResult := &models.Metrics{
					Path: "/metrics",
					Port: 9999,
					Containers: map[string]models.ContainerMetrics{
						"test": {
							Disabled: false,
							Port:     int32(8899),
							Path:     "/test/",
						},
					},
				}

				deploy := getWorkload("workload1", testNamespace)
				deploy.Spec.Metrics = &v1alpha1.ContainerMetricsConfiguration{
					Path: "/metrics", Port: 9999, Containers: map[string]*v1alpha1.MetricsConfigEntity{
						"test": {
							Port:     8899,
							Path:     "/test/",
							Disabled: false,
						},
					},
				}

				configMap.EXPECT().Fetch(gomock.Any(), gomock.Any(), gomock.Any()).Return(models.ConfigmapList{}, nil)
				repositoryMock.EXPECT().
					GetEdgeWorkload(gomock.Any(), "workload1", testNamespace).
					Return(deploy, nil)

				// when
				res := handler.GetDataMessageForDevice(deviceCtx, params)

				// then
				Expect(res).To(BeAssignableToTypeOf(&operations.GetDataMessageForDeviceOK{}))
				config := validateAndGetDeviceConfig(res)

				Expect(config.DeviceID).To(Equal(deviceName))
				Expect(config.Workloads).To(HaveLen(1))
				Expect(config.Workloads[0].Metrics).To(Equal(expectedResult))
			})

			It("Negative port is not considered", func() {

				// given
				deploy := getWorkload("workload1", testNamespace)
				deploy.Spec.Metrics = &v1alpha1.ContainerMetricsConfiguration{
					Path: "/metrics",
					Port: -1,
				}

				configMap.EXPECT().Fetch(gomock.Any(), gomock.Any(), gomock.Any()).Return(models.ConfigmapList{}, nil)
				repositoryMock.EXPECT().
					GetEdgeWorkload(gomock.Any(), "workload1", testNamespace).
					Return(deploy, nil)

				// when
				res := handler.GetDataMessageForDevice(deviceCtx, params)

				// then
				Expect(res).To(BeAssignableToTypeOf(&operations.GetDataMessageForDeviceOK{}))
				config := validateAndGetDeviceConfig(res)

				Expect(config.DeviceID).To(Equal(deviceName))
				Expect(config.Workloads).To(HaveLen(1))
				Expect(config.Workloads[0].Metrics).To(BeNil())
			})

			It("receiver empty configuration", func() {
				// given
				device.Status.Workloads = nil

				// when
				res := handler.GetDataMessageForDevice(deviceCtx, params)

				// then
				Expect(res).To(BeAssignableToTypeOf(&operations.GetDataMessageForDeviceOK{}))
				config := validateAndGetDeviceConfig(res)
				Expect(config.Configuration.Metrics).ToNot(BeNil())
				Expect(config.Configuration.Metrics.Receiver).To(Equal(k8s.GetDefaultMetricsReceiver()))
			})

			It("receiver full configuration", func() {
				// given
				caContent := "test"
				device.Status.Workloads = nil
				metricsReceiverConfig := &v1alpha1.MetricsReceiverConfiguration{
					URL:               "https://metricsreceiver.io:19291/api/v1/receive",
					RequestNumSamples: 1000,
					TimeoutSeconds:    32,
					CaSecretName:      caSecretName,
				}
				expectedConfig := &models.MetricsReceiverConfiguration{
					CaCert:            caContent,
					RequestNumSamples: metricsReceiverConfig.RequestNumSamples,
					TimeoutSeconds:    metricsReceiverConfig.TimeoutSeconds,
					URL:               metricsReceiverConfig.URL,
				}
				device.Spec.Metrics = &v1alpha1.MetricsConfiguration{
					ReceiverConfiguration: metricsReceiverConfig,
				}
				repositoryMock.EXPECT().GetSecret(gomock.Any(), caSecretName, testNamespace).
					Return(&corev1.Secret{
						Data: map[string][]byte{caSecretKeyName: []byte(caContent)},
					}, nil).
					Times(1)

				// when
				res := handler.GetDataMessageForDevice(deviceCtx, params)

				// then
				Expect(res).To(BeAssignableToTypeOf(&operations.GetDataMessageForDeviceOK{}))
				config := validateAndGetDeviceConfig(res)
				Expect(config.Configuration.Metrics).ToNot(BeNil())
				Expect(config.Configuration.Metrics.Receiver).To(Equal(expectedConfig))
			})

			It("receiver empty secret", func() {
				// given
				device.Status.Workloads = nil
				metricsReceiverConfig := &v1alpha1.MetricsReceiverConfiguration{
					URL:               "https://metricsreceiver.io:19291/api/v1/receive",
					RequestNumSamples: 1000,
					TimeoutSeconds:    32,
				}
				expectedConfig := &models.MetricsReceiverConfiguration{
					RequestNumSamples: metricsReceiverConfig.RequestNumSamples,
					TimeoutSeconds:    metricsReceiverConfig.TimeoutSeconds,
					URL:               metricsReceiverConfig.URL,
				}
				device.Spec.Metrics = &v1alpha1.MetricsConfiguration{
					ReceiverConfiguration: metricsReceiverConfig,
				}

				// when
				res := handler.GetDataMessageForDevice(deviceCtx, params)

				// then
				Expect(res).To(BeAssignableToTypeOf(&operations.GetDataMessageForDeviceOK{}))
				config := validateAndGetDeviceConfig(res)
				Expect(config.Configuration.Metrics).ToNot(BeNil())
				Expect(config.Configuration.Metrics.Receiver).To(Equal(expectedConfig))
			})

			It("receiver error reading secret", func() {
				// given
				device.Status.Workloads = nil
				metricsReceiverConfig := &v1alpha1.MetricsReceiverConfiguration{
					URL:               "https://metricsreceiver.io:19291/api/v1/receive",
					RequestNumSamples: 1000,
					TimeoutSeconds:    32,
					CaSecretName:      caSecretName,
				}
				device.Spec.Metrics = &v1alpha1.MetricsConfiguration{
					ReceiverConfiguration: metricsReceiverConfig,
				}
				repositoryMock.EXPECT().GetSecret(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil, errorNotFound).Times(1)

				// when
				res := handler.GetDataMessageForDevice(deviceCtx, params)

				// then
				Expect(res).To(BeAssignableToTypeOf(&operations.GetDataMessageForDeviceInternalServerError{}))
			})

		})

		It("Image registry authfile is included", func() {
			// given
			deviceName := "foo"
			device := getDevice(deviceName)
			device.Status.Workloads = []v1alpha1.Workload{{Name: "workload1"}}

			repositoryMock.EXPECT().
				GetEdgeDevice(gomock.Any(), deviceName, testNamespace).
				Return(device, nil).
				Times(1)

			workloadData := &v1alpha1.EdgeWorkload{
				ObjectMeta: v1.ObjectMeta{
					Name:      "workload1",
					Namespace: "default",
				},
				Spec: v1alpha1.EdgeWorkloadSpec{
					Type: "pod",
					Pod:  v1alpha1.Pod{},
					ImageRegistries: &v1alpha1.ImageRegistriesConfiguration{
						AuthFileSecret: &v1alpha1.NameRef{
							Name: "fooSecret",
						},
					},
				}}
			configMap.EXPECT().Fetch(gomock.Any(), gomock.Any(), gomock.Any()).Return(models.ConfigmapList{}, nil)
			repositoryMock.EXPECT().
				GetEdgeWorkload(gomock.Any(), "workload1", testNamespace).
				Return(workloadData, nil)

			authFileContent := "authfile-content"
			registryAuth.EXPECT().
				GetAuthFileFromSecret(gomock.Any(), gomock.Eq(workloadData.Namespace), gomock.Eq("fooSecret")).
				Return(authFileContent, nil)

			// when
			res := handler.GetDataMessageForDevice(deviceCtx, params)

			// then
			Expect(res).To(BeAssignableToTypeOf(&operations.GetDataMessageForDeviceOK{}))
			config := validateAndGetDeviceConfig(res)

			Expect(config.DeviceID).To(Equal(deviceName))
			Expect(config.Workloads).To(HaveLen(1))
			workload := config.Workloads[0]
			Expect(workload.Name).To(Equal("workload1"))
			Expect(workload.ImageRegistries).To(Not(BeNil()))
			Expect(workload.ImageRegistries.AuthFile).To(Equal(authFileContent))

			Expect(eventsRecorder.Events).ToNot(Receive())
		})

		It("Image registry authfile retrieval error", func() {
			// given
			deviceName := "foo"
			device := getDevice(deviceName)
			device.Status.Workloads = []v1alpha1.Workload{{Name: "workload1"}}

			repositoryMock.EXPECT().
				GetEdgeDevice(gomock.Any(), deviceName, testNamespace).
				Return(device, nil).
				Times(1)

			workloadData := &v1alpha1.EdgeWorkload{
				ObjectMeta: v1.ObjectMeta{
					Name:      "workload1",
					Namespace: "default",
				},
				Spec: v1alpha1.EdgeWorkloadSpec{
					Type: "pod",
					Pod:  v1alpha1.Pod{},
					ImageRegistries: &v1alpha1.ImageRegistriesConfiguration{
						AuthFileSecret: &v1alpha1.NameRef{
							Name: "fooSecret",
						},
					},
				}}
			repositoryMock.EXPECT().
				GetEdgeWorkload(gomock.Any(), "workload1", testNamespace).
				Return(workloadData, nil)

			registryAuth.EXPECT().
				GetAuthFileFromSecret(gomock.Any(), gomock.Eq("default"), gomock.Eq("fooSecret")).
				Return("", fmt.Errorf("failure"))

			// when
			res := handler.GetDataMessageForDevice(deviceCtx, params)

			// then
			Expect(res).To(BeAssignableToTypeOf(&operations.GetDataMessageForDeviceInternalServerError{}))

			Expect(eventsRecorder.Events).To(HaveLen(1))
			Expect(eventsRecorder.Events).To(Receive(ContainSubstring("Auth file secret")))
		})

		It("Secrets reading failed", func() {
			// given
			deviceName := "foo"
			device := getDevice(deviceName)
			device.Status.Workloads = []v1alpha1.Workload{{Name: "workload1"}}

			repositoryMock.EXPECT().
				GetEdgeDevice(gomock.Any(), deviceName, testNamespace).
				Return(device, nil).
				Times(1)

			secretName := "test"
			secretNamespacedName := types.NamespacedName{Namespace: device.Namespace, Name: secretName}
			podData := v1alpha1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test",
							Image: "test",
							EnvFrom: []corev1.EnvFromSource{
								{
									SecretRef: &corev1.SecretEnvSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: secretName,
										},
									},
								},
							},
						},
					},
				},
			}

			workloadData := &v1alpha1.EdgeWorkload{
				ObjectMeta: v1.ObjectMeta{
					Name:      "workload1",
					Namespace: "default",
				},
				Spec: v1alpha1.EdgeWorkloadSpec{
					DeviceSelector: &v1.LabelSelector{
						MatchLabels: map[string]string{"test": "test"},
					},
					Type: "pod",
					Pod:  podData,
					Data: &v1alpha1.DataConfiguration{},
				}}
			configMap.EXPECT().Fetch(gomock.Any(), gomock.Any(), gomock.Any()).Return(models.ConfigmapList{}, nil)
			repositoryMock.EXPECT().
				GetEdgeWorkload(gomock.Any(), "workload1", testNamespace).
				Return(workloadData, nil)
			repositoryMock.EXPECT().
				GetSecret(gomock.Any(), secretNamespacedName.Name, secretNamespacedName.Namespace).
				Return(nil, fmt.Errorf("test"))

			// when
			res := handler.GetDataMessageForDevice(deviceCtx, params)

			// then
			Expect(res).To(Equal(operations.NewGetDataMessageForDeviceInternalServerError()))
		})

		It("Secrets missing secret", func() {
			// given
			deviceName := "foo"
			device := getDevice(deviceName)
			device.Status.Workloads = []v1alpha1.Workload{{Name: "workload1"}}

			repositoryMock.EXPECT().
				GetEdgeDevice(gomock.Any(), deviceName, testNamespace).
				Return(device, nil).
				Times(1)

			secretName := "test"
			secretNamespacedName := types.NamespacedName{Namespace: device.Namespace, Name: secretName}
			podData := v1alpha1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test",
							Image: "test",
							EnvFrom: []corev1.EnvFromSource{
								{
									SecretRef: &corev1.SecretEnvSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: secretName,
										},
									},
								},
							},
						},
					},
				},
			}

			workloadData := &v1alpha1.EdgeWorkload{
				ObjectMeta: v1.ObjectMeta{
					Name:      "workload1",
					Namespace: "default",
				},
				Spec: v1alpha1.EdgeWorkloadSpec{
					DeviceSelector: &v1.LabelSelector{
						MatchLabels: map[string]string{"test": "test"},
					},
					Type: "pod",
					Pod:  podData,
					Data: &v1alpha1.DataConfiguration{},
				}}
			configMap.EXPECT().Fetch(gomock.Any(), gomock.Any(), gomock.Any()).Return(models.ConfigmapList{}, nil)
			repositoryMock.EXPECT().
				GetEdgeWorkload(gomock.Any(), "workload1", testNamespace).
				Return(workloadData, nil)
			repositoryMock.EXPECT().
				GetSecret(gomock.Any(), secretNamespacedName.Name, secretNamespacedName.Namespace).
				Return(nil, errorNotFound)

			// when
			res := handler.GetDataMessageForDevice(deviceCtx, params)

			// then
			Expect(res).To(Equal(operations.NewGetDataMessageForDeviceInternalServerError()))
		})

		It("Secrets partially optional secret", func() {
			// given
			deviceName := "foo"
			device := getDevice(deviceName)
			device.Status.Workloads = []v1alpha1.Workload{{Name: "workload1"}}

			repositoryMock.EXPECT().
				GetEdgeDevice(gomock.Any(), deviceName, testNamespace).
				Return(device, nil).
				Times(1)

			secretName := "test"
			secretNamespacedName := types.NamespacedName{Namespace: device.Namespace, Name: secretName}
			podData := v1alpha1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test",
							Image: "test",
							EnvFrom: []corev1.EnvFromSource{
								{
									SecretRef: &corev1.SecretEnvSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: secretName,
										},
									},
								},
							},
							Env: []corev1.EnvVar{
								{
									Name: "test",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: secretName,
											},
											Key:      "key",
											Optional: &boolTrue,
										},
									},
								},
							},
						},
					},
				},
			}

			workloadData := &v1alpha1.EdgeWorkload{
				ObjectMeta: v1.ObjectMeta{
					Name:      "workload1",
					Namespace: "default",
				},
				Spec: v1alpha1.EdgeWorkloadSpec{
					DeviceSelector: &v1.LabelSelector{
						MatchLabels: map[string]string{"test": "test"},
					},
					Type: "pod",
					Pod:  podData,
					Data: &v1alpha1.DataConfiguration{},
				}}
			configMap.EXPECT().Fetch(gomock.Any(), gomock.Any(), gomock.Any()).Return(models.ConfigmapList{}, nil)
			repositoryMock.EXPECT().
				GetEdgeWorkload(gomock.Any(), "workload1", testNamespace).
				Return(workloadData, nil)
			repositoryMock.EXPECT().
				GetSecret(gomock.Any(), secretNamespacedName.Name, secretNamespacedName.Namespace).
				Return(nil, errorNotFound)

			// when
			res := handler.GetDataMessageForDevice(deviceCtx, params)

			// then
			Expect(res).To(Equal(operations.NewGetDataMessageForDeviceInternalServerError()))
		})
		Context("Secrets missing secret key", func() {
			podData1 := v1alpha1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test",
							Image: "test",
							Env: []corev1.EnvVar{
								{
									Name: "test",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "secret",
											},
											Key: "key",
										},
									},
								},
								{
									Name: "test",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "secret",
											},
											Key: "key1",
										},
									},
								},
							},
						},
					},
				},
			}
			podData2 := v1alpha1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test",
							Image: "test",
							Env: []corev1.EnvVar{
								{
									Name: "test",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "secret",
											},
											Key: "key",
										},
									},
								},
								{
									Name: "test",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "secret",
											},
											Key:      "key",
											Optional: &boolTrue,
										},
									},
								},
							},
						},
					},
				},
			}
			podData3 := v1alpha1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test",
							Image: "test",
							EnvFrom: []corev1.EnvFromSource{
								{
									SecretRef: &corev1.SecretEnvSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "secret",
										},
									},
								},
							},
							Env: []corev1.EnvVar{
								{
									Name: "test",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "secret",
											},
											Key:      "key",
											Optional: &boolTrue,
										},
									},
								},
								{
									Name: "test",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "secret",
											},
											Key: "key",
										},
									},
								},
							},
						},
					},
				},
			}

			DescribeTable("Test table", func(podData *v1alpha1.Pod) {
				// given
				deviceName := "foo"
				device := getDevice(deviceName)
				device.Status.Workloads = []v1alpha1.Workload{{Name: "workload1"}}

				repositoryMock.EXPECT().
					GetEdgeDevice(gomock.Any(), deviceName, testNamespace).
					Return(device, nil).
					Times(1)

				workloadData := &v1alpha1.EdgeWorkload{
					ObjectMeta: v1.ObjectMeta{
						Name:      "workload1",
						Namespace: "default",
					},
					Spec: v1alpha1.EdgeWorkloadSpec{
						DeviceSelector: &v1.LabelSelector{
							MatchLabels: map[string]string{"test": "test"},
						},
						Type: "pod",
						Pod:  *podData,
						Data: &v1alpha1.DataConfiguration{},
					}}
				configMap.EXPECT().Fetch(gomock.Any(), gomock.Any(), gomock.Any()).Return(models.ConfigmapList{}, nil)
				repositoryMock.EXPECT().
					GetEdgeWorkload(gomock.Any(), "workload1", testNamespace).
					Return(workloadData, nil)
				secretDataMap := map[string][]byte{"key1": []byte("username"), "key2": []byte("password")}
				repositoryMock.EXPECT().
					GetSecret(gomock.Any(), "secret", device.Namespace).
					Return(&corev1.Secret{
						Data: secretDataMap,
					}, nil).
					Times(1)

				// when
				res := handler.GetDataMessageForDevice(deviceCtx, params)

				// then
				Expect(res).To(Equal(operations.NewGetDataMessageForDeviceInternalServerError()))
			},
				Entry("missing secret key", &podData1),
				Entry("partially optional secret key - mandatory appears first", &podData2),
				Entry("partially optional secret key - optional appears first", &podData3),
			)
		})

		It("Secrets reading succeeded", func() {
			// This test covers:
			// multiple workload
			// init containers and regular containers
			// secretRef and secretKeyRef
			// optional secretRef
			// optional secretKeyRef missing secret
			// optional secretKeyRef missing key
			// duplicate secret references

			// given
			deviceName := "foo"
			device := getDevice(deviceName)
			device.Status.Workloads = []v1alpha1.Workload{{Name: "workload1"}, {Name: "workload2"}}

			repositoryMock.EXPECT().
				GetEdgeDevice(gomock.Any(), deviceName, testNamespace).
				Return(device, nil).
				Times(1)

			podData1 := v1alpha1.Pod{
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{
							Name:  "ic1",
							Image: "test",
							EnvFrom: []corev1.EnvFromSource{
								{
									SecretRef: &corev1.SecretEnvSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "secret1",
										},
									},
								},
							},
							Env: []corev1.EnvVar{
								{
									Name: "test",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "secret1",
											},
											Key:      "notexist",
											Optional: &boolTrue,
										},
									},
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:  "c1",
							Image: "test",
							EnvFrom: []corev1.EnvFromSource{
								{
									SecretRef: &corev1.SecretEnvSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "secret2",
										},
									},
								},
							},
							Env: []corev1.EnvVar{
								{
									Name: "test",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "secret3",
											},
											Key: "key1",
										},
									},
								},
							},
						},
						{
							Name:  "c2",
							Image: "test",
							Env: []corev1.EnvVar{
								{
									Name: "test",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "secret4",
											},
											Key: "key1",
										},
									},
								},
							},
						},
					},
				},
			}
			podData2 := v1alpha1.Pod{
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{
							Name:  "ic1",
							Image: "test",
							Env: []corev1.EnvVar{
								{
									Name: "test",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "optional1",
											},
											Key:      "key1",
											Optional: &boolTrue,
										},
									},
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:  "c1",
							Image: "test",
							EnvFrom: []corev1.EnvFromSource{
								{
									SecretRef: &corev1.SecretEnvSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "optional2",
										},
										Optional: &boolTrue,
									},
								},
							},
						},
						{
							Name:  "c2",
							Image: "test",
							EnvFrom: []corev1.EnvFromSource{
								{
									SecretRef: &corev1.SecretEnvSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "secret1",
										},
									},
								},
								{
									SecretRef: &corev1.SecretEnvSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "secret5",
										},
									},
								},
							},
							Env: []corev1.EnvVar{
								{
									Name: "test",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "optional1",
											},
											Key:      "key1",
											Optional: &boolTrue,
										},
									},
								},
							},
						},
					},
				},
			}

			workloadData1 := &v1alpha1.EdgeWorkload{
				ObjectMeta: v1.ObjectMeta{
					Name:      "workload1",
					Namespace: "default",
				},
				Spec: v1alpha1.EdgeWorkloadSpec{
					DeviceSelector: &v1.LabelSelector{
						MatchLabels: map[string]string{"test": "test"},
					},
					Type: "pod",
					Pod:  podData1,
					Data: &v1alpha1.DataConfiguration{},
				}}
			workloadData2 := &v1alpha1.EdgeWorkload{
				ObjectMeta: v1.ObjectMeta{
					Name:      "workload2",
					Namespace: "default",
				},
				Spec: v1alpha1.EdgeWorkloadSpec{
					DeviceSelector: &v1.LabelSelector{
						MatchLabels: map[string]string{"test": "test"},
					},
					Type: "pod",
					Pod:  podData2,
					Data: &v1alpha1.DataConfiguration{},
				}}

			configMap.EXPECT().Fetch(gomock.Any(), gomock.Any(), gomock.Any()).Return(models.ConfigmapList{}, nil).AnyTimes()
			repositoryMock.EXPECT().
				GetEdgeWorkload(gomock.Any(), "workload1", testNamespace).
				Return(workloadData1, nil)
			repositoryMock.EXPECT().
				GetEdgeWorkload(gomock.Any(), "workload2", testNamespace).
				Return(workloadData2, nil)

			secretName := types.NamespacedName{
				Namespace: device.Namespace,
			}
			secretDataMap := map[string][]byte{"key1": []byte("username"), "key2": []byte("password")}
			secretDataJson := `{"key1":"dXNlcm5hbWU=","key2":"cGFzc3dvcmQ="}`
			secretName.Name = "secret1"
			repositoryMock.EXPECT().
				GetSecret(gomock.Any(), secretName.Name, secretName.Namespace).
				Return(&corev1.Secret{
					ObjectMeta: v1.ObjectMeta{Name: secretName.Name},
				}, nil).Times(1)
			secretName.Name = "secret2"
			repositoryMock.EXPECT().
				GetSecret(gomock.Any(), secretName.Name, secretName.Namespace).
				Return(&corev1.Secret{
					ObjectMeta: v1.ObjectMeta{Name: secretName.Name},
				}, nil).Times(1)
			secretName.Name = "secret3"
			repositoryMock.EXPECT().
				GetSecret(gomock.Any(), secretName.Name, gomock.Any()).
				Return(&corev1.Secret{
					ObjectMeta: v1.ObjectMeta{Name: secretName.Name},
					Data:       secretDataMap,
				}, nil).
				Times(1)
			secretName.Name = "secret4"
			repositoryMock.EXPECT().
				GetSecret(gomock.Any(), secretName.Name, secretName.Namespace).
				Return(&corev1.Secret{
					ObjectMeta: v1.ObjectMeta{Name: secretName.Name},
					Data:       secretDataMap,
				}, nil).
				Times(1)
			secretName.Name = "secret5"
			repositoryMock.EXPECT().
				GetSecret(gomock.Any(), secretName.Name, secretName.Namespace).
				Return(&corev1.Secret{
					ObjectMeta: v1.ObjectMeta{Name: secretName.Name},
				}, nil).Times(1)
			secretName.Name = "optional1"
			repositoryMock.EXPECT().
				GetSecret(gomock.Any(), secretName.Name, secretName.Namespace).
				Return(nil, errorNotFound).Times(1)
			secretName.Name = "optional2"
			repositoryMock.EXPECT().
				GetSecret(gomock.Any(), secretName.Name, secretName.Namespace).
				Return(nil, errorNotFound).Times(1)

			// when
			res := handler.GetDataMessageForDevice(deviceCtx, params)

			// then
			Expect(res).To(BeAssignableToTypeOf(&operations.GetDataMessageForDeviceOK{}))
			config := validateAndGetDeviceConfig(res)
			expectedList := []interface{}{
				&models.Secret{
					Name: "secret1",
					Data: "{}",
				},
				&models.Secret{
					Name: "secret2",
					Data: "{}",
				},
				&models.Secret{
					Name: "secret3",
					Data: secretDataJson,
				},
				&models.Secret{
					Name: "secret4",
					Data: secretDataJson,
				},
				&models.Secret{
					Name: "secret5",
					Data: "{}",
				},
			}
			Expect(config.Secrets).To(HaveLen(len(expectedList)))
			Expect(config.Secrets).To(ContainElements(expectedList...))
		})

		It("should map metrics retention configuration", func() {
			// given
			maxMiB := int32(123)
			maxHours := int32(24)

			device := getDevice("foo")
			device.Spec.Metrics = &v1alpha1.MetricsConfiguration{
				Retention: &v1alpha1.Retention{
					MaxMiB:   maxMiB,
					MaxHours: maxHours,
				},
			}

			repositoryMock.EXPECT().
				GetEdgeDevice(gomock.Any(), "foo", testNamespace).
				Return(device, nil).
				Times(1)

			// when
			res := handler.GetDataMessageForDevice(deviceCtx, params)

			// then
			Expect(res).To(BeAssignableToTypeOf(&operations.GetDataMessageForDeviceOK{}))
			config := validateAndGetDeviceConfig(res)

			Expect(config.Configuration.Metrics).ToNot(BeNil())
			Expect(config.Configuration.Metrics.Retention).ToNot(BeNil())
			Expect(config.Configuration.Metrics.Retention.MaxMib).To(Equal(maxMiB))
			Expect(config.Configuration.Metrics.Retention.MaxHours).To(Equal(maxHours))
		})

		When("system metrics", func() {

			It("should map metrics", func() {
				// given
				const allowListName = "a-name"
				interval := int32(3600)

				device := getDevice("foo")
				device.Spec.Metrics = &v1alpha1.MetricsConfiguration{
					SystemMetrics: &v1alpha1.ComponentMetricsConfiguration{
						Interval: interval,
						Disabled: true,
						AllowList: &v1alpha1.NameRef{
							Name: allowListName,
						},
					},
				}

				repositoryMock.EXPECT().
					GetEdgeDevice(gomock.Any(), "foo", testNamespace).
					Return(device, nil).
					Times(1)

				allowList := models.MetricsAllowList{Names: []string{"fizz", "buzz"}}
				allowListsMock.EXPECT().GenerateFromConfigMap(gomock.Any(), allowListName, device.Namespace).
					Return(&allowList, nil)

				// when
				res := handler.GetDataMessageForDevice(deviceCtx, params)

				// then
				Expect(res).To(BeAssignableToTypeOf(&operations.GetDataMessageForDeviceOK{}))
				config := validateAndGetDeviceConfig(res)

				Expect(config.Configuration.Metrics).ToNot(BeNil())
				Expect(config.Configuration.Metrics.System).ToNot(BeNil())

				Expect(config.Configuration.Metrics.System.Interval).To(Equal(interval))

				Expect(config.Configuration.Metrics.System.Disabled).To(BeTrue())

				Expect(config.Configuration.Metrics.System.AllowList).ToNot(BeNil())
				Expect(*config.Configuration.Metrics.System.AllowList).To(Equal(allowList))
			})

			It("should map mount configuration", func() {
				// given
				device := getDevice("foo")
				device.Spec.Mounts = []*v1alpha1.Mount{
					{
						Device:    "/dev/loop1",
						Directory: "/mnt",
						Type:      "ext4",
						Options:   "options",
					},
				}

				repositoryMock.EXPECT().
					GetEdgeDevice(gomock.Any(), "foo", testNamespace).
					Return(device, nil).
					Times(1)

				// when
				res := handler.GetDataMessageForDevice(deviceCtx, params)

				// then
				Expect(res).To(BeAssignableToTypeOf(&operations.GetDataMessageForDeviceOK{}))
				config := validateAndGetDeviceConfig(res)

				Expect(len(config.Configuration.Mounts)).To(Equal(1))
				Expect(config.Configuration.Mounts[0].Device).To(Equal(device.Spec.Mounts[0].Device))
				Expect(config.Configuration.Mounts[0].Directory).To(Equal(device.Spec.Mounts[0].Directory))
				Expect(config.Configuration.Mounts[0].Type).To(Equal(device.Spec.Mounts[0].Type))
				Expect(config.Configuration.Mounts[0].Options).To(Equal(device.Spec.Mounts[0].Options))
			})

			It("should fail when allow-list generation fails", func() {
				// given
				const allowListName = "a-name"

				device := getDevice("foo")
				device.Spec.Metrics = &v1alpha1.MetricsConfiguration{
					SystemMetrics: &v1alpha1.ComponentMetricsConfiguration{
						AllowList: &v1alpha1.NameRef{
							Name: allowListName,
						},
					},
				}

				allowListsMock.EXPECT().GenerateFromConfigMap(gomock.Any(), allowListName, device.Namespace).
					Return(nil, fmt.Errorf("boom!"))

				repositoryMock.EXPECT().
					GetEdgeDevice(gomock.Any(), "foo", testNamespace).
					Return(device, nil).
					Times(1)

				// when
				res := handler.GetDataMessageForDevice(deviceCtx, params)

				// then
				Expect(res).To(BeAssignableToTypeOf(&operations.GetDataMessageForDeviceInternalServerError{}))
			})
		})
		When("data transfer metrics", func() {

			It("should map metrics", func() {
				// given
				const allowListName = "a-name"
				interval := int32(3600)

				device := getDevice("foo")
				device.Spec.Metrics = &v1alpha1.MetricsConfiguration{
					DataTransferMetrics: &v1alpha1.ComponentMetricsConfiguration{
						Interval: interval,
						Disabled: true,
						AllowList: &v1alpha1.NameRef{
							Name: allowListName,
						},
					},
				}

				repositoryMock.EXPECT().
					GetEdgeDevice(gomock.Any(), "foo", testNamespace).
					Return(device, nil).
					Times(1)

				allowList := models.MetricsAllowList{Names: []string{"fizz", "buzz"}}
				allowListsMock.EXPECT().GenerateFromConfigMap(gomock.Any(), allowListName, device.Namespace).
					Return(&allowList, nil)

				// when
				res := handler.GetDataMessageForDevice(deviceCtx, params)

				// then
				Expect(res).To(BeAssignableToTypeOf(&operations.GetDataMessageForDeviceOK{}))
				config := validateAndGetDeviceConfig(res)

				Expect(config.Configuration.Metrics).ToNot(BeNil())
				Expect(config.Configuration.Metrics.DataTransfer).ToNot(BeNil())
				Expect(config.Configuration.Metrics.DataTransfer).To(Equal(
					&models.ComponentMetricsConfiguration{Interval: interval, Disabled: true, AllowList: &allowList}))

			})

			It("should fail when allow-list generation fails", func() {
				// given
				const allowListName = "a-name"

				device := getDevice("foo")
				device.Spec.Metrics = &v1alpha1.MetricsConfiguration{
					DataTransferMetrics: &v1alpha1.ComponentMetricsConfiguration{
						AllowList: &v1alpha1.NameRef{
							Name: allowListName,
						},
					},
				}

				allowListsMock.EXPECT().GenerateFromConfigMap(gomock.Any(), allowListName, device.Namespace).
					Return(nil, fmt.Errorf("boom!"))

				repositoryMock.EXPECT().
					GetEdgeDevice(gomock.Any(), "foo", testNamespace).
					Return(device, nil).
					Times(1)

				// when
				res := handler.GetDataMessageForDevice(deviceCtx, params)

				// then
				Expect(res).To(BeAssignableToTypeOf(&operations.GetDataMessageForDeviceInternalServerError{}))
			})
		})
		Context("With EdgeDeviceSet", func() {
			var (
				deviceName = "foo"
				setName    = "setFoo"
				deviceCtx  = context.WithValue(context.TODO(), AuthzKey, mtls.RequestAuthVal{CommonName: deviceName})
				device     *v1alpha1.EdgeDevice
				deviceSet  *v1alpha1.EdgeDeviceSet
				deploy     *v1alpha1.EdgeWorkload
			)

			getWorkload := func(name string, ns string) *v1alpha1.EdgeWorkload {
				return &v1alpha1.EdgeWorkload{
					ObjectMeta: v1.ObjectMeta{
						Name:      name,
						Namespace: ns,
					},
					Spec: v1alpha1.EdgeWorkloadSpec{
						Type: "pod",
						Pod:  v1alpha1.Pod{},
					},
				}
			}

			BeforeEach(func() {
				deviceName = "foo"
				device = getDevice(deviceName)
				device.Status.Workloads = []v1alpha1.Workload{{Name: "workload1"}}
				device.Labels = map[string]string{
					"flotta/member-of": setName,
				}

				device.Spec.LogCollection = map[string]*v1alpha1.LogCollectionConfig{
					"syslog-device": {
						Kind:         "syslog",
						BufferSize:   5,
						SyslogConfig: &v1alpha1.NameRef{Name: "syslog-config"},
					},
				}

				device.Spec.Heartbeat = &v1alpha1.HeartbeatConfiguration{
					PeriodSeconds: 1,
					HardwareProfile: &v1alpha1.HardwareProfileConfiguration{
						Include: false,
						Scope:   "full",
					},
				}

				device.Spec.Metrics = &v1alpha1.MetricsConfiguration{
					Retention: &v1alpha1.Retention{
						MaxMiB:   123,
						MaxHours: 123,
					},
					SystemMetrics: &v1alpha1.ComponentMetricsConfiguration{
						Interval:  123,
						Disabled:  false,
						AllowList: nil,
					},
				}

				device.Spec.OsInformation = &v1alpha1.OsInformation{
					AutomaticallyUpgrade: false,
					CommitID:             "fffffff",
					HostedObjectsURL:     "",
				}

				device.Spec.Storage = &v1alpha1.Storage{
					S3: &v1alpha1.S3Storage{
						SecretName:    "no-secret",
						ConfigMapName: "no-map",
						CreateOBC:     false,
					},
				}

				repositoryMock.EXPECT().
					GetEdgeDevice(gomock.Any(), deviceName, testNamespace).
					Return(device, nil).
					Times(1)

				deploy = getWorkload("workload1", testNamespace)
				repositoryMock.EXPECT().
					GetEdgeWorkload(gomock.Any(), "workload1", testNamespace).
					Return(deploy, nil)

				deviceSet = &v1alpha1.EdgeDeviceSet{
					TypeMeta:   v1.TypeMeta{},
					ObjectMeta: v1.ObjectMeta{Name: setName, Namespace: testNamespace},
					Spec:       v1alpha1.EdgeDeviceSetSpec{},
				}
				repositoryMock.EXPECT().
					GetEdgeDeviceSet(gomock.Any(), setName, testNamespace).
					Return(deviceSet, nil)

				configMap.EXPECT().Fetch(gomock.Any(), gomock.Any(), gomock.Any()).Return(models.ConfigmapList{}, nil)
			})

			It("should map log collection", func() {
				// given
				repositoryMock.EXPECT().GetConfigMap(
					gomock.Any(),
					"syslog-config", testNamespace).
					Return(&corev1.ConfigMap{
						Data: map[string]string{
							"Address": "127.0.0.1:512",
						},
					}, nil).
					Times(1)

				deviceSet.Spec.LogCollection = map[string]*v1alpha1.LogCollectionConfig{
					"syslog": {
						Kind:         "syslog",
						BufferSize:   10,
						SyslogConfig: &v1alpha1.NameRef{Name: "syslog-config"},
					},
				}
				deploy.Spec.LogCollection = "syslog"
				// when
				res := handler.GetDataMessageForDevice(deviceCtx, params)

				// then
				Expect(res).To(BeAssignableToTypeOf(&operations.GetDataMessageForDeviceOK{}))
				config := validateAndGetDeviceConfig(res)

				Expect(config.DeviceID).To(Equal(deviceName))

				// Device Log config
				Expect(config.Configuration.LogCollection).To(HaveKey("syslog"))
				logConfig := config.Configuration.LogCollection["syslog"]
				Expect(logConfig.Kind).To(Equal("syslog"))
				Expect(logConfig.BufferSize).To(Equal(int32(10)))
				Expect(logConfig.SyslogConfig.Address).To(Equal("127.0.0.1:512"))
				Expect(logConfig.SyslogConfig.Protocol).To(Equal("tcp"))

				Expect(config.Workloads).To(HaveLen(1))
				workload := config.Workloads[0]
				Expect(workload.Name).To(Equal("workload1"))
				Expect(workload.LogCollection).To(Equal("syslog"))
				Expect(workload.ImageRegistries).To(BeNil())
			})

			It("should map metrics", func() {
				// given
				const allowListName = "a-name"
				interval := int32(3600)
				maxMiB := int32(123)
				maxHours := int32(24)

				deviceSet.Spec.Metrics = &v1alpha1.MetricsConfiguration{
					Retention: &v1alpha1.Retention{
						MaxMiB:   maxMiB,
						MaxHours: maxHours,
					},
					SystemMetrics: &v1alpha1.ComponentMetricsConfiguration{
						Interval: interval,
						Disabled: true,
						AllowList: &v1alpha1.NameRef{
							Name: allowListName,
						},
					},
				}

				allowList := models.MetricsAllowList{Names: []string{"fizz", "buzz"}}
				allowListsMock.EXPECT().GenerateFromConfigMap(gomock.Any(), allowListName, device.Namespace).
					Return(&allowList, nil)

				// when
				res := handler.GetDataMessageForDevice(deviceCtx, params)

				// then
				Expect(res).To(BeAssignableToTypeOf(&operations.GetDataMessageForDeviceOK{}))
				config := validateAndGetDeviceConfig(res)

				Expect(config.Configuration.Metrics).ToNot(BeNil())

				Expect(config.Configuration.Metrics.Retention).ToNot(BeNil())
				Expect(config.Configuration.Metrics.Retention.MaxHours).To(Equal(maxHours))
				Expect(config.Configuration.Metrics.Retention.MaxMib).To(Equal(maxMiB))

				Expect(config.Configuration.Metrics.System).ToNot(BeNil())
				Expect(config.Configuration.Metrics.System.Interval).To(Equal(interval))
				Expect(config.Configuration.Metrics.System.Disabled).To(BeTrue())

				Expect(config.Configuration.Metrics.System.AllowList).ToNot(BeNil())
				Expect(*config.Configuration.Metrics.System.AllowList).To(Equal(allowList))
			})

			It("should map heartbeat", func() {
				// given
				period := int64(123)
				deviceSet.Spec.Heartbeat = &v1alpha1.HeartbeatConfiguration{
					PeriodSeconds: period,
					HardwareProfile: &v1alpha1.HardwareProfileConfiguration{
						Include: true,
						Scope:   "delta",
					},
				}

				// when
				res := handler.GetDataMessageForDevice(deviceCtx, params)

				// then
				Expect(res).To(BeAssignableToTypeOf(&operations.GetDataMessageForDeviceOK{}))
				config := validateAndGetDeviceConfig(res)

				Expect(config.Configuration.Heartbeat).ToNot(BeNil())
				Expect(config.Configuration.Heartbeat.PeriodSeconds).To(Equal(period))
				Expect(config.Configuration.Heartbeat.HardwareProfile).ToNot(BeNil())
				Expect(config.Configuration.Heartbeat.HardwareProfile.Include).To(BeTrue())
				Expect(config.Configuration.Heartbeat.HardwareProfile.Scope).To(Equal("delta"))
			})

			It("should override OS information", func() {
				// given
				commitID := "12345"
				url := "http://images.io"
				deviceSet.Spec.OsInformation = &v1alpha1.OsInformation{
					AutomaticallyUpgrade: true,
					CommitID:             commitID,
					HostedObjectsURL:     url,
				}

				// when
				res := handler.GetDataMessageForDevice(deviceCtx, params)

				// then
				Expect(res).To(BeAssignableToTypeOf(&operations.GetDataMessageForDeviceOK{}))
				config := validateAndGetDeviceConfig(res)

				Expect(config.Configuration.Os).ToNot(BeNil())
				Expect(config.Configuration.Os.AutomaticallyUpgrade).To(BeTrue())
				Expect(config.Configuration.Os.CommitID).To(Equal(commitID))
				Expect(config.Configuration.Os.HostedObjectsURL).To(Equal(url))
			})

			It("should override (remove) S3 storage information", func() {
				// given
				deviceSet.Spec.Storage = &v1alpha1.Storage{
					S3: &v1alpha1.S3Storage{
						CreateOBC: true,
					},
				}

				// when
				res := handler.GetDataMessageForDevice(deviceCtx, params)

				// then
				Expect(res).To(BeAssignableToTypeOf(&operations.GetDataMessageForDeviceOK{}))
				config := validateAndGetDeviceConfig(res)

				Expect(config.Configuration.Storage).To(BeNil())
			})
		})
		Context("workload annotations and labels", func() {
			It("should propagate Labels with prefix 'podman/'", func() {
				// given
				deviceName := "foo"
				device := getDevice(deviceName)
				device.Status.Workloads = []v1alpha1.Workload{{Name: "workload1"}}

				repositoryMock.EXPECT().
					GetEdgeDevice(gomock.Any(), deviceName, testNamespace).
					Return(device, nil).
					Times(1)

				workloadData := &v1alpha1.EdgeWorkload{
					ObjectMeta: v1.ObjectMeta{
						Name:      "workload1",
						Namespace: "default",
						Labels:    map[string]string{"podman/label1": "1"},
					},
					Spec: v1alpha1.EdgeWorkloadSpec{
						Type: "pod",
						Pod:  v1alpha1.Pod{},
					}}
				configMap.EXPECT().Fetch(gomock.Any(), gomock.Any(), gomock.Any()).Return(models.ConfigmapList{}, nil)
				repositoryMock.EXPECT().
					GetEdgeWorkload(gomock.Any(), "workload1", testNamespace).
					Return(workloadData, nil)
				// when
				res := handler.GetDataMessageForDevice(deviceCtx, params)

				// then
				Expect(res).To(BeAssignableToTypeOf(&operations.GetDataMessageForDeviceOK{}))
				config := validateAndGetDeviceConfig(res)

				Expect(config.DeviceID).To(Equal(deviceName))
				Expect(config.Workloads).To(HaveLen(1))
				workload := config.Workloads[0]
				Expect(workload.Name).To(Equal("workload1"))
				Expect(workload.Labels).To(Equal(map[string]string{"label1": "1"}))
			})

			It("should propagate Annotations with prefix 'podman/'", func() {
				// given
				deviceName := "foo"
				device := getDevice(deviceName)
				device.Status.Workloads = []v1alpha1.Workload{{Name: "workload1"}}

				repositoryMock.EXPECT().
					GetEdgeDevice(gomock.Any(), deviceName, testNamespace).
					Return(device, nil).
					Times(1)

				workloadData := &v1alpha1.EdgeWorkload{
					ObjectMeta: v1.ObjectMeta{
						Name:        "workload1",
						Namespace:   "default",
						Annotations: map[string]string{"podman/annotate1": "1"},
					},
					Spec: v1alpha1.EdgeWorkloadSpec{
						Type: "pod",
						Pod:  v1alpha1.Pod{},
					}}
				configMap.EXPECT().Fetch(gomock.Any(), gomock.Any(), gomock.Any()).Return(models.ConfigmapList{}, nil)
				repositoryMock.EXPECT().
					GetEdgeWorkload(gomock.Any(), "workload1", testNamespace).
					Return(workloadData, nil)
				// when
				res := handler.GetDataMessageForDevice(deviceCtx, params)

				// then
				Expect(res).To(BeAssignableToTypeOf(&operations.GetDataMessageForDeviceOK{}))
				config := validateAndGetDeviceConfig(res)

				Expect(config.DeviceID).To(Equal(deviceName))
				Expect(config.Workloads).To(HaveLen(1))
				workload := config.Workloads[0]
				Expect(workload.Name).To(Equal("workload1"))
				Expect(workload.Annotations).To(Equal(map[string]string{"annotate1": "1"}))
			})
		})
	})

	Context("PostDataMessageForDevice", func() {

		var (
			deviceName string
			device     *v1alpha1.EdgeDevice
			deviceCtx  context.Context
		)

		BeforeEach(func() {
			deviceName = "foo"
			deviceCtx = context.WithValue(context.TODO(), AuthzKey, mtls.RequestAuthVal{CommonName: deviceName})
			device = getDevice(deviceName)
		})

		It("Invalid params", func() {
			// given
			params := api.PostDataMessageForDeviceParams{
				DeviceID: deviceName,
				Message: &models.Message{
					Directive: "NOT VALID ONE",
				},
			}

			// when
			res := handler.PostDataMessageForDevice(deviceCtx, params)

			// then
			Expect(res).To(BeAssignableToTypeOf(&api.PostDataMessageForDeviceBadRequest{}))
		})

		It("Invalid deviceID on context", func() {
			// given
			ctx := context.WithValue(context.TODO(), AuthzKey, mtls.RequestAuthVal{CommonName: "bar"})
			params := api.PostDataMessageForDeviceParams{
				DeviceID: deviceName,
				Message: &models.Message{
					Directive: "NOT VALID ONE",
				},
			}
			metricsMock.EXPECT().IncEdgeDeviceInvalidOwnerCounter().Times(2)

			// when
			res := handler.PostDataMessageForDevice(ctx, params)
			emptyRes := handler.PostDataMessageForDevice(context.TODO(), params)

			// then
			Expect(res).To(BeAssignableToTypeOf(&api.PostDataMessageForDeviceForbidden{}))
			Expect(emptyRes).To(BeAssignableToTypeOf(&api.PostDataMessageForDeviceForbidden{}))
		})

		Context("Heartbeat", func() {
			var directiveName = "heartbeat"

			It("invalid deviceID on context", func() {
				// given

				ctx := context.WithValue(context.TODO(), AuthzKey, mtls.RequestAuthVal{CommonName: "bar"})

				params := api.PostDataMessageForDeviceParams{
					DeviceID: deviceName,
					Message: &models.Message{
						Directive: directiveName,
					},
				}
				metricsMock.EXPECT().IncEdgeDeviceInvalidOwnerCounter().Times(1)

				// when
				res := handler.PostDataMessageForDevice(ctx, params)

				// then
				Expect(res).To(BeAssignableToTypeOf(&api.PostDataMessageForDeviceForbidden{}))
			})

			It("Device not found", func() {
				// given
				repositoryMock.EXPECT().
					GetEdgeDevice(gomock.Any(), deviceName, testNamespace).
					Return(nil, errorNotFound).
					Times(1)

				params := api.PostDataMessageForDeviceParams{
					DeviceID: deviceName,
					Message: &models.Message{
						Directive: directiveName,
					},
				}

				// when
				res := handler.PostDataMessageForDevice(deviceCtx, params)

				// then
				Expect(res).To(BeAssignableToTypeOf(&api.PostDataMessageForDeviceNotFound{}))
			})

			It("Device cannot be retrieved", func() {
				// given
				repositoryMock.EXPECT().
					GetEdgeDevice(gomock.Any(), deviceName, testNamespace).
					Return(nil, fmt.Errorf("failed")).
					Times(4)

				params := api.PostDataMessageForDeviceParams{
					DeviceID: deviceName,
					Message: &models.Message{
						Directive: directiveName,
					},
				}

				// when
				res := handler.PostDataMessageForDevice(deviceCtx, params)

				// then
				Expect(res).To(BeAssignableToTypeOf(&api.PostDataMessageForDeviceInternalServerError{}))
			})

			It("Work without content", func() {
				// given
				repositoryMock.EXPECT().
					GetEdgeDevice(gomock.Any(), deviceName, testNamespace).
					Return(device, nil).
					Times(1)

				repositoryMock.EXPECT().
					PatchEdgeDeviceStatus(gomock.Any(), device, gomock.Any()).
					Return(nil).
					Times(0)

				repositoryMock.EXPECT().
					UpdateEdgeDeviceLabels(gomock.Any(), device, gomock.Any()).
					Return(nil).
					Times(1)

				metricsMock.EXPECT().
					RecordEdgeDevicePresence(device.Namespace, device.Name).
					Times(1)

				params := api.PostDataMessageForDeviceParams{
					DeviceID: deviceName,
					Message: &models.Message{
						Directive: directiveName,
					},
				}

				// when
				res := handler.PostDataMessageForDevice(deviceCtx, params)

				// then
				Expect(res).To(BeAssignableToTypeOf(&api.PostDataMessageForDeviceOK{}))
			})

			It("Work with content", func() {
				// given
				content := models.Heartbeat{
					Status:  "running",
					Version: "1",
					Workloads: []*models.WorkloadStatus{
						{Name: "workload-1", Status: "running"}},
					Hardware: &models.HardwareInfo{
						Hostname: "test-hostname",
					},
				}

				device.Status.Workloads = []v1alpha1.Workload{{
					Name:  "workload-1",
					Phase: "failing",
				}}

				repositoryMock.EXPECT().
					GetEdgeDevice(gomock.Any(), deviceName, testNamespace).
					Return(device, nil).
					Times(1)

				repositoryMock.EXPECT().
					UpdateEdgeDeviceLabels(gomock.Any(), device, gomock.Any()).
					Return(nil).
					Times(1)

				repositoryMock.EXPECT().
					PatchEdgeDeviceStatus(gomock.Any(), device, gomock.Any()).
					Do(func(ctx context.Context, edgeDevice *v1alpha1.EdgeDevice, patch *client.Patch) {
						Expect(edgeDevice.Status.Workloads).To(HaveLen(1))
						Expect(edgeDevice.Status.Workloads[0].Phase).To(
							Equal(v1alpha1.EdgeWorkloadPhase("running")))
						Expect(edgeDevice.Status.Workloads[0].Name).To(Equal("workload-1"))
					}).
					Return(nil).
					Times(1)

				metricsMock.EXPECT().
					RecordEdgeDevicePresence(device.Namespace, device.Name).
					Times(1)

				params := api.PostDataMessageForDeviceParams{
					DeviceID: deviceName,
					Message: &models.Message{
						Directive: directiveName,
						Content:   content,
					},
				}

				// when
				res := handler.PostDataMessageForDevice(deviceCtx, params)

				// then
				Expect(res).To(BeAssignableToTypeOf(&api.PostDataMessageForDeviceOK{}))
			})

			It("Don't patch status for no changes", func() {
				// given
				content := models.Heartbeat{
					Status:  "running",
					Version: "1",
					Workloads: []*models.WorkloadStatus{
						{Name: "workload-1", Status: "running"}},
					Hardware: &models.HardwareInfo{
						Hostname: "test-hostname",
					},
				}
				device.Status.Phase = content.Status
				device.Status.Workloads = []v1alpha1.Workload{{
					Name:  "workload-1",
					Phase: "running",
				}}
				device.Status.LastSyncedResourceVersion = content.Version
				device.Status.Hardware = &v1alpha1.Hardware{
					Hostname:    content.Hardware.Hostname,
					Disks:       []*v1alpha1.Disk{},
					Interfaces:  []*v1alpha1.Interface{},
					Gpus:        []*v1alpha1.Gpu{},
					HostDevices: []*v1alpha1.HostDevice{},
					Mounts:      []*v1alpha1.Mount{},
				}

				repositoryMock.EXPECT().
					GetEdgeDevice(gomock.Any(), deviceName, testNamespace).
					Return(device, nil).
					Times(1)

				repositoryMock.EXPECT().
					UpdateEdgeDeviceLabels(gomock.Any(), device, gomock.Any()).
					Return(nil).
					Times(1)

				repositoryMock.EXPECT().
					PatchEdgeDeviceStatus(gomock.Any(), device, gomock.Any()).
					Return(nil).
					Times(0)

				metricsMock.EXPECT().
					RecordEdgeDevicePresence(device.Namespace, device.Name).
					Times(1)

				params := api.PostDataMessageForDeviceParams{
					DeviceID: deviceName,
					Message: &models.Message{
						Directive: directiveName,
						Content:   content,
					},
				}

				// when
				res := handler.PostDataMessageForDevice(deviceCtx, params)

				// then
				Expect(res).To(BeAssignableToTypeOf(&api.PostDataMessageForDeviceOK{}))
			})

			It("Work with content and events", func() {
				// given
				content := models.Heartbeat{
					Status:  "running",
					Version: "1",
					Workloads: []*models.WorkloadStatus{
						{Name: "workload-1", Status: "created"}},
					Hardware: &models.HardwareInfo{
						Hostname: "test-hostname",
					},
					Events: []*models.EventInfo{{
						Message: "failed to start container",
						Reason:  "Started",
						Type:    models.EventInfoTypeWarn,
					}},
				}

				device.Status.Workloads = []v1alpha1.Workload{{
					Name:  "workload-1",
					Phase: "failing",
				}}

				repositoryMock.EXPECT().
					GetEdgeDevice(gomock.Any(), deviceName, testNamespace).
					Return(device, nil).
					Times(1)

				repositoryMock.EXPECT().
					UpdateEdgeDeviceLabels(gomock.Any(), device, gomock.Any()).
					Return(nil).
					Times(1)

				repositoryMock.EXPECT().
					PatchEdgeDeviceStatus(gomock.Any(), device, gomock.Any()).
					Do(func(ctx context.Context, edgeDevice *v1alpha1.EdgeDevice, patch *client.Patch) {
						Expect(edgeDevice.Status.Workloads).To(HaveLen(1))
						Expect(edgeDevice.Status.Workloads[0].Phase).To(
							Equal(v1alpha1.EdgeWorkloadPhase("created")))
						Expect(edgeDevice.Status.Workloads[0].Name).To(Equal("workload-1"))
					}).
					Return(nil).
					Times(1)

				metricsMock.EXPECT().
					RecordEdgeDevicePresence(device.Namespace, device.Name).
					Times(1)

				params := api.PostDataMessageForDeviceParams{
					DeviceID: deviceName,
					Message: &models.Message{
						Directive: directiveName,
						Content:   content,
					},
				}

				// when
				res := handler.PostDataMessageForDevice(deviceCtx, params)

				// test emmiting the events:
				close(eventsRecorder.Events)
				found := false
				for event := range eventsRecorder.Events {
					if strings.Contains(event, "failed to start container") {
						found = true
					}
				}
				Expect(found).To(BeTrue())

				// then
				Expect(res).To(BeAssignableToTypeOf(&api.PostDataMessageForDeviceOK{}))
			})

			It("Fail on invalid content", func() {
				// given
				content := "invalid"

				params := api.PostDataMessageForDeviceParams{
					DeviceID: deviceName,
					Message: &models.Message{
						Directive: directiveName,
						Content:   content,
					},
				}

				// when
				res := handler.PostDataMessageForDevice(deviceCtx, params)

				// then
				Expect(res).To(BeAssignableToTypeOf(&api.PostDataMessageForDeviceBadRequest{}))
			})

			It("Fail on update device status", func() {
				// given
				content := models.Heartbeat{
					Version: "1",
				}

				// updateDeviceStatus try to patch the status 4 times, and Read the
				// device from repo too.
				repositoryMock.EXPECT().
					GetEdgeDevice(gomock.Any(), deviceName, testNamespace).
					DoAndReturn(func(_, _, _ interface{}) (*v1alpha1.EdgeDevice, error) {
						return device.DeepCopy(), nil
					}).
					Times(4)

				patched := device.DeepCopy()
				patched.Status.LastSyncedResourceVersion = content.Version
				repositoryMock.EXPECT().
					PatchEdgeDeviceStatus(gomock.Any(), patched, gomock.Any()).
					Return(fmt.Errorf("Failed")).
					Times(4)

				metricsMock.EXPECT().
					RecordEdgeDevicePresence(device.Namespace, device.Name).
					AnyTimes()

				params := api.PostDataMessageForDeviceParams{
					DeviceID: deviceName,
					Message: &models.Message{
						Directive: directiveName,
						Content:   content,
					},
				}

				// when
				res := handler.PostDataMessageForDevice(deviceCtx, params)

				// then
				Expect(res).To(BeAssignableToTypeOf(&api.PostDataMessageForDeviceInternalServerError{}))
			})

			It("Update device status retries if any error", func() {
				// given
				// updateDeviceStatus try to patch the status 4 times, and Read the
				// device from repo too, in this case will retry 2 times.
				content := models.Heartbeat{
					Version: "1",
				}

				repositoryMock.EXPECT().
					GetEdgeDevice(gomock.Any(), deviceName, testNamespace).
					DoAndReturn(func(_, _, _ interface{}) (*v1alpha1.EdgeDevice, error) {
						return device.DeepCopy(), nil
					}).
					Times(4)

				patched := device.DeepCopy()
				patched.Status.LastSyncedResourceVersion = content.Version
				repositoryMock.EXPECT().
					PatchEdgeDeviceStatus(gomock.Any(), patched, gomock.Any()).
					Return(fmt.Errorf("Failed")).
					Times(3)

				repositoryMock.EXPECT().
					PatchEdgeDeviceStatus(gomock.Any(), patched, gomock.Any()).
					Return(nil).
					Times(1)

				repositoryMock.EXPECT().
					UpdateEdgeDeviceLabels(gomock.Any(), patched, gomock.Any()).
					Return(nil).
					Times(1)

				metricsMock.EXPECT().
					RecordEdgeDevicePresence(device.Namespace, device.Name).
					AnyTimes()

				params := api.PostDataMessageForDeviceParams{
					DeviceID: deviceName,
					Message: &models.Message{
						Directive: directiveName,
						Content:   content,
					},
				}

				// when
				res := handler.PostDataMessageForDevice(deviceCtx, params)

				// then
				Expect(res).To(BeAssignableToTypeOf(&api.PostDataMessageForDeviceOK{}))
			})
		})

		Context("enrolment", func() {
			var (
				directiveName   = "enrolment"
				targetNamespace = "fooNS"
			)

			It("Empty enrolement information does not crash", func() {
				// given
				repositoryMock.EXPECT().
					GetEdgeDevice(gomock.Any(), deviceName, testNamespace).
					Return(device, nil).
					Times(1)

				params := api.PostDataMessageForDeviceParams{
					DeviceID: deviceName,
					Message: &models.Message{
						Directive: directiveName,
					},
				}

				// when
				res := handler.PostDataMessageForDevice(deviceCtx, params)

				// then
				Expect(res).To(BeAssignableToTypeOf(&api.PostDataMessageForDeviceAlreadyReported{}))
			})

			It("It's already created", func() {
				// given
				repositoryMock.EXPECT().
					GetEdgeDevice(gomock.Any(), deviceName, targetNamespace).
					Return(device, nil).
					Times(1)

				params := api.PostDataMessageForDeviceParams{
					DeviceID: deviceName,
					Message: &models.Message{
						Directive: directiveName,
						Content: models.EnrolmentInfo{
							Features:        &models.EnrolmentInfoFeatures{},
							TargetNamespace: &targetNamespace,
						},
					},
				}

				// when
				res := handler.PostDataMessageForDevice(deviceCtx, params)

				// then
				Expect(res).To(BeAssignableToTypeOf(&api.PostDataMessageForDeviceAlreadyReported{}))
			})

			It("Submitted correctly", func() {
				// given
				repositoryMock.EXPECT().
					GetEdgeDeviceSignedRequest(gomock.Any(), deviceName, device.Namespace).
					Return(nil, fmt.Errorf("Failed")).
					Times(1)

				repositoryMock.EXPECT().
					CreateEdgeDeviceSignedRequest(gomock.Any(), gomock.Any()).
					Do(func(ctx context.Context, edgedeviceSignedRequest *v1alpha1.EdgeDeviceSignedRequest) error {
						Expect(edgedeviceSignedRequest.Name).To(Equal(deviceName))
						Expect(edgedeviceSignedRequest.Spec.TargetNamespace).To(Equal(targetNamespace))
						return nil
					}).
					Times(1)

				repositoryMock.EXPECT().
					GetEdgeDevice(gomock.Any(), deviceName, targetNamespace).
					Return(nil, fmt.Errorf("Failed")).
					Times(1)

				params := api.PostDataMessageForDeviceParams{
					DeviceID: deviceName,
					Message: &models.Message{
						Directive: directiveName,
						Content: models.EnrolmentInfo{
							Features:        &models.EnrolmentInfoFeatures{},
							TargetNamespace: &targetNamespace,
						},
					},
				}

				// when
				res := handler.PostDataMessageForDevice(deviceCtx, params)

				// then
				Expect(res).To(BeAssignableToTypeOf(&api.PostDataMessageForDeviceOK{}))
			})

			It("Create edgedevice signer request failed", func() {
				// given
				repositoryMock.EXPECT().
					GetEdgeDeviceSignedRequest(gomock.Any(), deviceName, device.Namespace).
					Return(nil, fmt.Errorf("Failed")).
					Times(1)

				repositoryMock.EXPECT().
					CreateEdgeDeviceSignedRequest(gomock.Any(), gomock.Any()).
					Return(fmt.Errorf("failed")).
					Times(1)

				repositoryMock.EXPECT().
					GetEdgeDevice(gomock.Any(), deviceName, targetNamespace).
					Return(nil, fmt.Errorf("Failed")).
					Times(1)

				params := api.PostDataMessageForDeviceParams{
					DeviceID: deviceName,
					Message: &models.Message{
						Directive: directiveName,
						Content: models.EnrolmentInfo{
							Features:        &models.EnrolmentInfoFeatures{},
							TargetNamespace: &targetNamespace,
						},
					},
				}

				// when
				res := handler.PostDataMessageForDevice(deviceCtx, params)

				// then
				Expect(res).To(BeAssignableToTypeOf(&api.PostDataMessageForDeviceBadRequest{}))
			})

			It("edgedevice signed request is already created with same namespace", func() {
				// given
				edsr := &v1alpha1.EdgeDeviceSignedRequest{
					Spec: v1alpha1.EdgeDeviceSignedRequestSpec{
						TargetNamespace: targetNamespace,
						Approved:        true,
					},
				}

				repositoryMock.EXPECT().
					GetEdgeDeviceSignedRequest(gomock.Any(), deviceName, device.Namespace).
					Return(edsr, nil).
					Times(1)

				repositoryMock.EXPECT().
					GetEdgeDevice(gomock.Any(), deviceName, targetNamespace).
					Return(nil, fmt.Errorf("Failed")).
					Times(1)

				params := api.PostDataMessageForDeviceParams{
					DeviceID: deviceName,
					Message: &models.Message{
						Directive: directiveName,
						Content: models.EnrolmentInfo{
							Features:        &models.EnrolmentInfoFeatures{},
							TargetNamespace: &targetNamespace,
						},
					},
				}

				// when
				res := handler.PostDataMessageForDevice(deviceCtx, params)

				// then
				Expect(res).To(BeAssignableToTypeOf(&api.PostDataMessageForDeviceOK{}))
			})

			It("edgedevice signed request is in different namespace", func() {
				// given
				edsr := &v1alpha1.EdgeDeviceSignedRequest{
					Spec: v1alpha1.EdgeDeviceSignedRequestSpec{
						TargetNamespace: "newOne",
						Approved:        true,
					},
				}

				repositoryMock.EXPECT().
					GetEdgeDeviceSignedRequest(gomock.Any(), deviceName, device.Namespace).
					Return(edsr, nil).
					Times(1)

				repositoryMock.EXPECT().
					GetEdgeDevice(gomock.Any(), deviceName, targetNamespace).
					Return(nil, fmt.Errorf("Failed")).
					Times(1)

				repositoryMock.EXPECT().
					GetEdgeDevice(gomock.Any(), deviceName, "newOne").
					Return(nil, nil).
					Times(1)

				params := api.PostDataMessageForDeviceParams{
					DeviceID: deviceName,
					Message: &models.Message{
						Directive: directiveName,
						Content: models.EnrolmentInfo{
							Features:        &models.EnrolmentInfoFeatures{},
							TargetNamespace: &targetNamespace,
						},
					},
				}

				// when
				res := handler.PostDataMessageForDevice(deviceCtx, params)

				// then
				Expect(res).To(BeAssignableToTypeOf(&api.PostDataMessageForDeviceAlreadyReported{}))
			})

			It("edgedevice signed request is in different namespace but device is not yet created", func() {
				// given
				edsr := &v1alpha1.EdgeDeviceSignedRequest{
					Spec: v1alpha1.EdgeDeviceSignedRequestSpec{
						TargetNamespace: "newOne",
						Approved:        true,
					},
				}

				repositoryMock.EXPECT().
					GetEdgeDeviceSignedRequest(gomock.Any(), deviceName, device.Namespace).
					Return(edsr, nil).
					Times(1)

				repositoryMock.EXPECT().
					GetEdgeDevice(gomock.Any(), deviceName, targetNamespace).
					Return(nil, fmt.Errorf("Failed")).
					Times(1)

				repositoryMock.EXPECT().
					GetEdgeDevice(gomock.Any(), deviceName, "newOne").
					Return(nil, fmt.Errorf("Failed")).
					Times(1)

				params := api.PostDataMessageForDeviceParams{
					DeviceID: deviceName,
					Message: &models.Message{
						Directive: directiveName,
						Content: models.EnrolmentInfo{
							Features:        &models.EnrolmentInfoFeatures{},
							TargetNamespace: &targetNamespace,
						},
					},
				}

				// when
				res := handler.PostDataMessageForDevice(deviceCtx, params)

				// then
				Expect(res).To(BeAssignableToTypeOf(&api.PostDataMessageForDeviceOK{}))
			})
		})

		Context("Registration", func() {
			var directiveName = "registration"

			Context("With certificate", func() {

				createCSR := func() []byte {
					keys, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
					ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Cannot create key")
					var csrTemplate = x509.CertificateRequest{
						Version: 0,
						Subject: pkix.Name{
							CommonName:   "test",
							Organization: []string{"k4e"},
						},
						SignatureAlgorithm: x509.ECDSAWithSHA256,
					}
					// step: generate the csr request
					csrCertificate, err := x509.CreateCertificateRequest(rand.Reader, &csrTemplate, keys)
					Expect(err).NotTo(HaveOccurred())
					return csrCertificate
				}

				getCert := func(PayloadContent interface{}) *x509.Certificate {
					content, ok := PayloadContent.(models.RegistrationResponse)
					Expect(ok).To(BeTrue())

					block, rest := pem.Decode([]byte(content.Certificate))
					Expect(block).NotTo(BeNil())
					Expect(rest).NotTo(BeNil())

					cert, err := x509.ParseCertificate(block.Bytes)
					Expect(err).NotTo(HaveOccurred())
					return cert
				}

				var givenCert string

				BeforeEach(func() {
					initKubeConfig()
					MTLSConfig := mtls.NewMTLSConfig(k8sClient, testNamespace, []string{"foo.com"}, true)
					assembler := k8s.NewConfigurationAssembler(
						allowListsMock,
						nil,
						configMap,
						eventsRecorder,
						registryAuth,
						repositoryMock,
					)
					backend := k8s.NewBackend(repositoryMock, assembler, logger.Sugar(), testNamespace, eventsRecorder)
					handler = yggdrasil.NewYggdrasilHandler(testNamespace, metricsMock, MTLSConfig, logger.Sugar(), backend, edgeDeviceRepoMock, playbookExecRepoMock)
					_, _, err := MTLSConfig.InitCertificates()
					Expect(err).ToNot(HaveOccurred())

					givenCert = string(pem.EncodeToMemory(&pem.Block{
						Type:  "CERTIFICATE",
						Bytes: createCSR(),
					}))

				})

				AfterEach(func() {
					err := testEnv.Stop()
					Expect(err).NotTo(HaveOccurred())
				})

				It("No error on repo read, but there is no device", func() {

					// given
					repositoryMock.EXPECT().
						GetEdgeDevice(gomock.Any(), deviceName, testNamespace).
						Return(nil, nil).
						Times(1)

					metricsMock.EXPECT().
						IncEdgeDeviceFailedRegistration().
						Times(1)

					params := api.PostDataMessageForDeviceParams{
						DeviceID: deviceName,
						Message: &models.Message{
							Directive: directiveName,
							Content: models.RegistrationInfo{
								CertificateRequest: string(givenCert),
								Hardware:           nil,
							},
						},
					}

					// when
					res := handler.PostDataMessageForDevice(deviceCtx, params)

					// then

					Expect(res).To(BeAssignableToTypeOf(&api.PostDataMessageForDeviceInternalServerError{}))
				})

				It("Device register for first time", func() {

					// given

					// KEY and Value hardcoded here because a change can break any update.
					key := "edgedeviceSignedRequest" // v1alpha1.EdgeDeviceSignedRequestLabelName
					val := "true"                    // v1alpha1.EdgeDeviceSignedRequestLabelValue
					device.ObjectMeta.Labels = map[string]string{key: val}

					repositoryMock.EXPECT().
						GetEdgeDevice(gomock.Any(), deviceName, testNamespace).
						Return(device, nil).
						Times(2)

					repositoryMock.EXPECT().
						PatchEdgeDevice(gomock.Any(), gomock.Any(), gomock.Any()).
						Do(func(ctx context.Context, edgeDevice *v1alpha1.EdgeDevice, dvcCopy *v1alpha1.EdgeDevice) {
							Expect(dvcCopy.ObjectMeta.Labels).To(HaveLen(1))
							Expect(dvcCopy.ObjectMeta.Labels).To(Equal(map[string]string{
								"device.hostname": "testfoo"}))
							Expect(dvcCopy.ObjectMeta.Finalizers).To(HaveLen(0))
						}).
						Return(nil).
						Times(1)

					repositoryMock.EXPECT().
						PatchEdgeDeviceStatus(gomock.Any(), device, gomock.Any()).
						Return(nil).
						Times(1)

					metricsMock.EXPECT().
						IncEdgeDeviceSuccessfulRegistration().
						Times(1)

					params := api.PostDataMessageForDeviceParams{
						DeviceID: deviceName,
						Message: &models.Message{
							Directive: directiveName,
							Content: models.RegistrationInfo{
								CertificateRequest: string(givenCert),
								Hardware: &models.HardwareInfo{
									Hostname: "testFoo",
								},
							},
						},
					}

					// when
					res := handler.PostDataMessageForDevice(deviceCtx, params)

					// then
					Expect(res).To(BeAssignableToTypeOf(&api.PostDataMessageForDeviceOK{}))
					data, ok := res.(*operations.PostDataMessageForDeviceOK)
					Expect(ok).To(BeTrue())
					Expect(data.Payload.Content).NotTo(BeNil())

					cert := getCert(data.Payload.Content)
					Expect(cert.Subject.CommonName).To(Equal("foo"))
					Expect(cert.Subject.OrganizationalUnit).To(HaveLen(1))
					Expect(cert.Subject.OrganizationalUnit).To(ContainElement("test-ns"))
				})

				It("Device register for first time, but EDSR says another namespace", func() {

					// given
					key := "edgedeviceSignedRequest" // v1alpha1.EdgeDeviceSignedRequestLabelName
					val := "true"                    // v1alpha1.EdgeDeviceSignedRequestLabelValue
					device.ObjectMeta.Labels = map[string]string{key: val}
					otherNS := "otherNS"
					edsr := &v1alpha1.EdgeDeviceSignedRequest{
						Spec: v1alpha1.EdgeDeviceSignedRequestSpec{
							TargetNamespace: otherNS,
							Approved:        true,
						},
					}

					repositoryMock.EXPECT().
						GetEdgeDeviceSignedRequest(gomock.Any(), deviceName, testNamespace).
						Return(edsr, nil).
						Times(1)

					repositoryMock.EXPECT().
						GetEdgeDevice(gomock.Any(), deviceName, otherNS).
						Return(device, nil).
						Times(2)

					repositoryMock.EXPECT().
						PatchEdgeDevice(gomock.Any(), gomock.Any(), gomock.Any()).
						Return(nil).
						Times(1)

					repositoryMock.EXPECT().
						PatchEdgeDeviceStatus(gomock.Any(), device, gomock.Any()).
						Return(nil).
						Times(1)

					metricsMock.EXPECT().
						IncEdgeDeviceSuccessfulRegistration().
						Times(1)

					params := api.PostDataMessageForDeviceParams{
						DeviceID: deviceName,
						Message: &models.Message{
							Directive: directiveName,
							Content: models.RegistrationInfo{
								CertificateRequest: string(givenCert),
								Hardware: &models.HardwareInfo{
									Hostname: "testFoo",
								},
							},
						},
					}

					// when
					res := handler.PostDataMessageForDevice(context.TODO(), params)

					// then
					Expect(res).To(BeAssignableToTypeOf(&api.PostDataMessageForDeviceOK{}))
					data, ok := res.(*operations.PostDataMessageForDeviceOK)
					Expect(ok).To(BeTrue())
					Expect(data.Payload.Content).NotTo(BeNil())

					cert := getCert(data.Payload.Content)
					Expect(cert.Subject.CommonName).To(Equal("foo"))
					Expect(cert.Subject.OrganizationalUnit).To(HaveLen(1))
					Expect(cert.Subject.OrganizationalUnit).To(ContainElement("otherNS"))
				})

				It("Device is already register, and send a CSR to renew", func() {
					// given
					repositoryMock.EXPECT().
						GetEdgeDevice(gomock.Any(), deviceName, testNamespace).
						Return(device, nil).
						Times(2)

					repositoryMock.EXPECT().
						PatchEdgeDevice(gomock.Any(), gomock.Any(), gomock.Any()).
						Return(nil).
						Times(1)

					repositoryMock.EXPECT().
						PatchEdgeDeviceStatus(gomock.Any(), device, gomock.Any()).
						Return(nil).
						Times(1)

					metricsMock.EXPECT().
						IncEdgeDeviceSuccessfulRegistration().
						Times(1)

					params := api.PostDataMessageForDeviceParams{
						DeviceID: deviceName,
						Message: &models.Message{
							Directive: directiveName,
							Content: models.RegistrationInfo{
								CertificateRequest: string(givenCert),
								Hardware:           nil,
							},
						},
					}

					// when
					res := handler.PostDataMessageForDevice(deviceCtx, params)

					// then
					Expect(res).To(BeAssignableToTypeOf(&api.PostDataMessageForDeviceOK{}))
					data, ok := res.(*operations.PostDataMessageForDeviceOK)
					Expect(ok).To(BeTrue())
					Expect(data.Payload.Content).NotTo(BeNil())

					cert := getCert(data.Payload.Content)
					Expect(cert.Subject.CommonName).To(Equal("foo"))
					Expect(cert.Subject.OrganizationalUnit).To(HaveLen(1))
					Expect(cert.Subject.OrganizationalUnit).To(ContainElement("test-ns"))
				})

				It("cannot patch device", func() {
					// given
					repositoryMock.EXPECT().
						GetEdgeDevice(gomock.Any(), deviceName, testNamespace).
						Return(device, nil).
						Times(2)

					repositoryMock.EXPECT().
						PatchEdgeDevice(gomock.Any(), gomock.Any(), gomock.Any()).
						Return(fmt.Errorf("Failed")).
						Times(1)

					metricsMock.EXPECT().
						IncEdgeDeviceFailedRegistration().
						Times(1)

					params := api.PostDataMessageForDeviceParams{
						DeviceID: deviceName,
						Message: &models.Message{
							Directive: directiveName,
							Content: models.RegistrationInfo{
								CertificateRequest: string(givenCert),
								Hardware:           nil,
							},
						},
					}

					// when
					res := handler.PostDataMessageForDevice(deviceCtx, params)

					// then
					Expect(res).To(BeAssignableToTypeOf(&api.PostDataMessageForDeviceInternalServerError{}))
				})

				It("try to update a device that it's not his own", func() {
					// given
					repositoryMock.EXPECT().
						GetEdgeDeviceSignedRequest(gomock.Any(), deviceName, device.Namespace).
						Return(nil, errorNotFound).
						Times(1)

					metricsMock.EXPECT().
						IncEdgeDeviceFailedRegistration().
						Times(1)

					params := api.PostDataMessageForDeviceParams{
						DeviceID: deviceName,
						Message: &models.Message{
							Directive: directiveName,
							Content: models.RegistrationInfo{
								CertificateRequest: string(givenCert),
								Hardware:           nil,
							},
						},
					}

					// when
					res := handler.PostDataMessageForDevice(
						context.WithValue(context.TODO(), AuthzKey, mtls.RequestAuthVal{CommonName: "bar"}),
						params)

					// then
					Expect(res).To(BeAssignableToTypeOf(&api.PostDataMessageForDeviceNotFound{}))
				})

				It("Device is already register, and send a CSR to renew with invalid cert", func() {
					// given
					repositoryMock.EXPECT().
						GetEdgeDevice(gomock.Any(), deviceName, testNamespace).
						Return(device, nil).
						Times(1)

					params := api.PostDataMessageForDeviceParams{
						DeviceID: deviceName,
						Message: &models.Message{
							Directive: directiveName,
							Content: models.RegistrationInfo{
								CertificateRequest: string("----Invalid-----"),
								Hardware:           nil,
							},
						},
					}

					// when
					res := handler.PostDataMessageForDevice(deviceCtx, params)

					// then
					Expect(res).To(BeAssignableToTypeOf(&api.PostDataMessageForDeviceBadRequest{}))
				})

				It("Device is not registered, and send a valid CSR, but not approved", func() {

					// given
					repositoryMock.EXPECT().
						GetEdgeDevice(gomock.Any(), deviceName, testNamespace).
						Return(nil, errorNotFound).
						Times(1)

					params := api.PostDataMessageForDeviceParams{
						DeviceID: deviceName,
						Message: &models.Message{
							Directive: directiveName,
							Content: models.RegistrationInfo{
								CertificateRequest: string(givenCert),
								Hardware:           nil,
							},
						},
					}

					// when
					res := handler.PostDataMessageForDevice(deviceCtx, params)

					// then
					Expect(res).To(BeAssignableToTypeOf(&api.PostDataMessageForDeviceNotFound{}))
				})

				It("Update device status failed", func() {
					// retry on status is already tested on heartbeat section
					// given
					repositoryMock.EXPECT().
						GetEdgeDevice(gomock.Any(), deviceName, testNamespace).
						Return(device, nil).
						Times(5)

					repositoryMock.EXPECT().
						PatchEdgeDeviceStatus(gomock.Any(), gomock.Any(), gomock.Any()).
						Do(func(ctx context.Context, edgeDevice *v1alpha1.EdgeDevice, patch *client.Patch) {
							Expect(edgeDevice.Name).To(Equal(deviceName))
							Expect(edgeDevice.Namespace).To(Equal(testNamespace))
							Expect(edgeDevice.Status.Workloads).To(HaveLen(0))
						}).
						Return(fmt.Errorf("Failed")).
						Times(4)

					repositoryMock.EXPECT().
						PatchEdgeDevice(gomock.Any(), gomock.Any(), gomock.Any()).
						Return(nil).
						Times(1)

					metricsMock.EXPECT().
						IncEdgeDeviceFailedRegistration().
						AnyTimes()

					params := api.PostDataMessageForDeviceParams{
						DeviceID: deviceName,
						Message: &models.Message{
							Directive: directiveName,
							Content: models.RegistrationInfo{
								CertificateRequest: givenCert,
							},
						},
					}

					// when
					res := handler.PostDataMessageForDevice(deviceCtx, params)

					// then
					Expect(res).To(BeAssignableToTypeOf(&api.PostDataMessageForDeviceInternalServerError{}))
				})

			})

			It("Read device from repository failed", func() {
				// given
				repositoryMock.EXPECT().
					GetEdgeDevice(gomock.Any(), deviceName, testNamespace).
					Return(nil, fmt.Errorf("Failed")).
					Times(1)

				metricsMock.EXPECT().
					IncEdgeDeviceFailedRegistration().
					Times(1)

				params := api.PostDataMessageForDeviceParams{
					DeviceID: deviceName,
					Message: &models.Message{
						Directive: directiveName,
					},
				}

				// when
				res := handler.PostDataMessageForDevice(deviceCtx, params)

				// then
				Expect(res).To(BeAssignableToTypeOf(&api.PostDataMessageForDeviceInternalServerError{}))
			})

			It("Create device with invalid content", func() {
				// given
				content := "Invalid--"
				params := api.PostDataMessageForDeviceParams{
					DeviceID: deviceName,
					Message: &models.Message{
						Directive: directiveName,
						Content:   &content,
					},
				}

				// when
				res := handler.PostDataMessageForDevice(deviceCtx, params)

				// then
				Expect(res).To(BeAssignableToTypeOf(&api.PostDataMessageForDeviceBadRequest{}))
			})

		})

	})
})

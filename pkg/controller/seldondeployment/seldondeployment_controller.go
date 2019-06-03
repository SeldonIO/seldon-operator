/*
Copyright 2019 The Seldon Team.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package seldondeployment

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/knative/pkg/apis/istio/common/v1alpha1"
	istio "github.com/knative/pkg/apis/istio/v1alpha3"
	machinelearningv1alpha2 "github.com/seldonio/seldon-operator/pkg/apis/machinelearning/v1alpha2"
	appsv1 "k8s.io/api/apps/v1"
	autoscaling "k8s.io/api/autoscaling/v2beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"os"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
	"strconv"
)

const (
	ENV_DEFAULT_ENGINE_SERVER_PORT      = "ENGINE_SERVER_PORT"
	ENV_DEFAULT_ENGINE_SERVER_GRPC_PORT = "ENGINE_SERVER_GRPC_PORT"

	DEFAULT_ENGINE_CONTAINER_PORT = 8000
	DEFAULT_ENGINE_GRPC_PORT      = 5001
)

var log = logf.Log.WithName("seldon-controller")

// Add creates a new SeldonDeployment Controller and adds it to the Manager with default RBAC. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileSeldonDeployment{Client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("seldondeployment-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to SeldonDeployment
	err = c.Watch(&source.Kind{Type: &machinelearningv1alpha2.SeldonDeployment{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// Watch for Deployments owned by a SeldonDeployment
	err = c.Watch(&source.Kind{Type: &appsv1.Deployment{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &machinelearningv1alpha2.SeldonDeployment{},
	})
	if err != nil {
		return err
	}

	// Watch for Services owned by a SeldonDeployment
	err = c.Watch(&source.Kind{Type: &corev1.Service{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &machinelearningv1alpha2.SeldonDeployment{},
	})
	if err != nil {
		return err
	}

	if getEnv(ENV_ISTIO_ENABLED, "false") == "true" {
		err = c.Watch(&source.Kind{Type: &istio.VirtualService{}}, &handler.EnqueueRequestForOwner{
			IsController: true,
			OwnerType:    &machinelearningv1alpha2.SeldonDeployment{},
		})
		if err != nil {
			return err
		}
	}

	return nil
}

var _ reconcile.Reconciler = &ReconcileSeldonDeployment{}

// ReconcileSeldonDeployment reconciles a SeldonDeployment object
type ReconcileSeldonDeployment struct {
	client.Client
	scheme *runtime.Scheme
}

type components struct {
	serviceDetails  map[string]*machinelearningv1alpha2.ServiceStatus
	deployments     []*appsv1.Deployment
	services        []*corev1.Service
	hpas            []*autoscaling.HorizontalPodAutoscaler
	virtualService  []*istio.VirtualService
	destinationRule *istio.DestinationRule
}

type serviceDetails struct {
	svcName        string
	deploymentName string
	svcUrl         string
	ambassadorUrl  string
}

// Get the Namespace from the SeldonDeployment. Returns "default" if none found.
func getNamespace(deployment *machinelearningv1alpha2.SeldonDeployment) string {
	if len(deployment.ObjectMeta.Namespace) > 0 {
		return deployment.ObjectMeta.Namespace
	} else {
		return "default"
	}
}

// Translte the PredictorSpec p in to base64 encoded JSON.
func getEngineVarJson(p *machinelearningv1alpha2.PredictorSpec) (string, error) {
	str, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(str), nil
}

// Get an environment variable given by key or return the fallback.
func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

// Get an annotation from the Seldon Deployment given by annotationKey or return the fallback.
func getAnnotation(mlDep *machinelearningv1alpha2.SeldonDeployment, annotationKey string, fallback string) string {
	if annotation, hasAnnotation := mlDep.Spec.Annotations[annotationKey]; hasAnnotation {
		return annotation
	} else {
		return fallback
	}
}

// Create the Container for the service orchestrator.
func createEngineContainer(mlDep *machinelearningv1alpha2.SeldonDeployment, p *machinelearningv1alpha2.PredictorSpec, engine_http_port, engine_grpc_port int) (*corev1.Container, error) {
	// Get engine user
	var engineUser int64 = -1
	if engineUserEnv, ok := os.LookupEnv("ENGINE_CONTAINER_USER"); ok {
		user, err := strconv.Atoi(engineUserEnv)
		if err != nil {
			return nil, err
		} else {
			engineUser = int64(user)
		}
	}
	// get predictor as base64 encoded json
	predictorB64, err := getEngineVarJson(p)
	if err != nil {
		return nil, err
	}

	//get annotation for java opts or default
	javaOpts := getAnnotation(mlDep, machinelearningv1alpha2.ANNOTATION_JAVA_OPTS, "-Dcom.sun.management.jmxremote.rmi.port=9090 -Dcom.sun.management.jmxremote -Dcom.sun.management.jmxremote.port=9090 -Dcom.sun.management.jmxremote.ssl=false -Dcom.sun.management.jmxremote.authenticate=false -Dcom.sun.management.jmxremote.local.only=false -Djava.rmi.server.hostname=127.0.0.1")

	//Engine resources
	engineResources := p.SvcOrchSpec.Resources
	if engineResources == nil {
		cpuQuantity, _ := resource.ParseQuantity("0.1")
		engineResources = &corev1.ResourceRequirements{
			Requests: map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceCPU: cpuQuantity,
			},
		}
	}

	c := corev1.Container{
		Name:                     "seldon-container-engine",
		Image:                    getEnv("ENGINE_CONTAINER_IMAGE_AND_VERSION", "seldonio/engine:0.2.7-SNAPSHOT"),
		ImagePullPolicy:          corev1.PullPolicy(getEnv("ENGINE_CONTAINER_IMAGE_PULL_POLICY", "IfNotPresent")),
		TerminationMessagePath:   "/dev/termination-log",
		TerminationMessagePolicy: corev1.TerminationMessageReadFile,
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      machinelearningv1alpha2.PODINFO_VOLUME_NAME,
				MountPath: machinelearningv1alpha2.PODINFO_VOLUME_PATH,
			},
		},
		Env: []corev1.EnvVar{
			{Name: "ENGINE_PREDICTOR", Value: predictorB64},
			{Name: "DEPLOYMENT_NAME", Value: mlDep.Spec.Name},
			{Name: "ENGINE_SERVER_PORT", Value: strconv.Itoa(engine_http_port)},
			{Name: "ENGINE_SERVER_GRPC_PORT", Value: strconv.Itoa(engine_grpc_port)},
			{Name: "JAVA_OPTS", Value: javaOpts},
		},
		Ports: []corev1.ContainerPort{
			{ContainerPort: int32(engine_http_port), Protocol: corev1.ProtocolTCP},
			{ContainerPort: int32(engine_grpc_port), Protocol: corev1.ProtocolTCP},
			{ContainerPort: 8082, Name: "admin", Protocol: corev1.ProtocolTCP},
			{ContainerPort: 9090, Name: "jmx", Protocol: corev1.ProtocolTCP},
		},
		ReadinessProbe: &corev1.Probe{Handler: corev1.Handler{HTTPGet: &corev1.HTTPGetAction{Port: intstr.FromString("admin"), Path: "/ready", Scheme: corev1.URISchemeHTTP}},
			InitialDelaySeconds: 20,
			PeriodSeconds:       1,
			FailureThreshold:    1,
			SuccessThreshold:    1,
			TimeoutSeconds:      2},
		LivenessProbe: &corev1.Probe{Handler: corev1.Handler{HTTPGet: &corev1.HTTPGetAction{Port: intstr.FromString("admin"), Path: "/ready", Scheme: corev1.URISchemeHTTP}},
			InitialDelaySeconds: 20,
			PeriodSeconds:       5,
			FailureThreshold:    3,
			SuccessThreshold:    1,
			TimeoutSeconds:      2},
		Lifecycle: &corev1.Lifecycle{
			PreStop: &corev1.Handler{
				Exec: &corev1.ExecAction{Command: []string{"/bin/sh", "-c", "curl 127.0.0.1:" + strconv.Itoa(engine_http_port) + "/pause; /bin/sleep 10"}},
			},
		},
		Resources: *engineResources,
	}
	if engineUser != -1 {
		var procMount = corev1.DefaultProcMount
		c.SecurityContext = &corev1.SecurityContext{RunAsUser: &engineUser, ProcMount: &procMount}
	}
	// Environment vars if specified
	if p.SvcOrchSpec.Env != nil {
		for _, env := range p.SvcOrchSpec.Env {
			c.Env = append(c.Env, *env)
		}
	}

	return &c, nil
}

// Create the service orchestrator.
func createEngineDeployment(mlDep *machinelearningv1alpha2.SeldonDeployment, p *machinelearningv1alpha2.PredictorSpec, seldonId string, engine_http_port, engine_grpc_port int) (*appsv1.Deployment, error) {

	var terminationGracePeriodSecs = int64(20)
	var defaultMode = corev1.DownwardAPIVolumeSourceDefaultMode

	depName := machinelearningv1alpha2.GetServiceOrchestratorName(mlDep, p)

	con, err := createEngineContainer(mlDep, p, engine_http_port, engine_grpc_port)
	if err != nil {
		return nil, err
	}
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:        depName,
			Namespace:   getNamespace(mlDep),
			Labels:      map[string]string{machinelearningv1alpha2.Label_svc_orch: "true", machinelearningv1alpha2.Label_seldon_app: seldonId, machinelearningv1alpha2.Label_seldon_id: seldonId, "app": depName, "version": "v1"},
			Annotations: mlDep.Spec.Annotations,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{machinelearningv1alpha2.Label_seldon_app: seldonId, machinelearningv1alpha2.Label_seldon_id: seldonId},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{machinelearningv1alpha2.Label_seldon_app: seldonId, machinelearningv1alpha2.Label_seldon_id: seldonId, "app": depName},
					Annotations: map[string]string{
						"prometheus.io/path":   getEnv("ENGINE_PROMETHEUS_PATH", "/prometheus"),
						"prometheus.io/port":   strconv.Itoa(engine_http_port),
						"prometheus.io/scrape": "true",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						*con,
					},
					TerminationGracePeriodSeconds: &terminationGracePeriodSecs,
					DNSPolicy:                     corev1.DNSClusterFirst,
					SchedulerName:                 "default-scheduler",
					SecurityContext:               &corev1.PodSecurityContext{},
					Volumes: []corev1.Volume{
						{Name: machinelearningv1alpha2.PODINFO_VOLUME_NAME, VolumeSource: corev1.VolumeSource{DownwardAPI: &corev1.DownwardAPIVolumeSource{Items: []corev1.DownwardAPIVolumeFile{
							{Path: "annotations", FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.annotations", APIVersion: "v1"}},
						}, DefaultMode: &defaultMode}}},
					},
					RestartPolicy: corev1.RestartPolicyAlways,
				},
			},
			Strategy: appsv1.DeploymentStrategy{RollingUpdate: &appsv1.RollingUpdateDeployment{MaxUnavailable: &intstr.IntOrString{StrVal: "10%"}}},
			Replicas: &p.Replicas,
		},
	}

	// Add a particular service account rather than default for the engine
	if svcAccount, ok := os.LookupEnv("ENGINE_CONTAINER_SERVICE_ACCOUNT_NAME"); ok {
		deploy.Spec.Template.Spec.ServiceAccountName = svcAccount
		deploy.Spec.Template.Spec.DeprecatedServiceAccount = svcAccount
	} else {
		deploy.Spec.Template.Spec.ServiceAccountName = "default"
		deploy.Spec.Template.Spec.DeprecatedServiceAccount = "default"
	}

	// add predictor labels
	for k, v := range p.Labels {
		deploy.ObjectMeta.Labels[k] = v
		deploy.Spec.Template.ObjectMeta.Labels[k] = v
	}

	return deploy, nil
}

func createHpa(podSpec *machinelearningv1alpha2.SeldonPodSpec, deploymentName string, seldonId string, namespace string) *autoscaling.HorizontalPodAutoscaler {
	hpa := autoscaling.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentName,
			Namespace: namespace,
			Labels:    map[string]string{machinelearningv1alpha2.Label_seldon_id: seldonId},
		},
		Spec: autoscaling.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscaling.CrossVersionObjectReference{
				Name:       deploymentName,
				APIVersion: "extensions/v1beta1",
				Kind:       "Deployment",
			},
			MaxReplicas: podSpec.HpaSpec.MaxReplicas,
			Metrics:     podSpec.HpaSpec.Metrics,
		},
	}
	if podSpec.HpaSpec.MinReplicas != nil {
		hpa.Spec.MinReplicas = podSpec.HpaSpec.MinReplicas
	}

	return &hpa
}

// Create istio virtual service and destination rule.
// Creates routes for each predictor with traffic weight split
func createIstioResources(mlDep *machinelearningv1alpha2.SeldonDeployment,
	seldonId string,
	namespace string,
	engine_http_port int,
	engine_grpc_port int) ([]*istio.VirtualService, *istio.DestinationRule) {

	istio_gateway := getEnv(ENV_ISTIO_GATEWAY, "seldon-gateway")
	httpVsvc := &istio.VirtualService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      seldonId + "-http",
			Namespace: namespace,
		},
		Spec: istio.VirtualServiceSpec{
			Hosts:    []string{"*"},
			Gateways: []string{getAnnotation(mlDep, ANNOTATION_ISTIO_GATEWAY, istio_gateway)},
			HTTP: []istio.HTTPRoute{
				{
					Match: []istio.HTTPMatchRequest{
						{
							URI: &v1alpha1.StringMatch{Prefix: "/seldon/" + namespace + "/" + mlDep.Name + "/"},
						},
					},
					Rewrite: &istio.HTTPRewrite{URI: "/"},
				},
			},
		},
	}

	grpcVsvc := &istio.VirtualService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      seldonId + "-grpc",
			Namespace: namespace,
		},
		Spec: istio.VirtualServiceSpec{
			Hosts:    []string{"*"},
			Gateways: []string{getAnnotation(mlDep, ANNOTATION_ISTIO_GATEWAY, "seldon-gateway")},
			HTTP: []istio.HTTPRoute{
				{
					Match: []istio.HTTPMatchRequest{
						{
							URI: &v1alpha1.StringMatch{Prefix: "/seldon.protos.Seldon/"},
							Headers: map[string]v1alpha1.StringMatch{
								"seldon":    v1alpha1.StringMatch{Exact: mlDep.Name},
								"namespace": v1alpha1.StringMatch{Exact: namespace},
							},
						},
					},
				},
			},
		},
	}

	drule := &istio.DestinationRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      seldonId,
			Namespace: namespace,
		},
		Spec: istio.DestinationRuleSpec{
			Host: seldonId,
		},
	}

	routesHttp := make([]istio.HTTPRouteDestination, len(mlDep.Spec.Predictors))
	routesGrpc := make([]istio.HTTPRouteDestination, len(mlDep.Spec.Predictors))
	subsets := make([]istio.Subset, len(mlDep.Spec.Predictors))
	for i := 0; i < len(mlDep.Spec.Predictors); i++ {
		p := mlDep.Spec.Predictors[i]
		routesHttp[i] = istio.HTTPRouteDestination{
			Destination: istio.Destination{
				Host:   seldonId,
				Subset: p.Name,
				Port: istio.PortSelector{
					Number: uint32(engine_http_port),
				},
			},
			Weight: int(p.Traffic),
		}
		routesGrpc[i] = istio.HTTPRouteDestination{
			Destination: istio.Destination{
				Host:   seldonId,
				Subset: p.Name,
				Port: istio.PortSelector{
					Number: uint32(engine_grpc_port),
				},
			},
			Weight: int(p.Traffic),
		}
		subsets[i] = istio.Subset{
			Name: p.Name,
			Labels: map[string]string{
				"version": p.Labels["version"],
			},
		}
	}
	httpVsvc.Spec.HTTP[0].Route = routesHttp
	grpcVsvc.Spec.HTTP[0].Route = routesGrpc
	drule.Spec.Subsets = subsets
	vscs := make([]*istio.VirtualService, 2)
	vscs[0] = httpVsvc
	vscs[1] = grpcVsvc

	return vscs, drule
}

// Create all the components (Deployments, Services etc)
func createComponents(mlDep *machinelearningv1alpha2.SeldonDeployment) (*components, error) {
	c := components{}
	c.serviceDetails = map[string]*machinelearningv1alpha2.ServiceStatus{}
	seldonId := machinelearningv1alpha2.GetSeldonDeploymentName(mlDep)
	namespace := getNamespace(mlDep)
	var err error
	// Get engine http port from environment or use default
	var engine_http_port = DEFAULT_ENGINE_CONTAINER_PORT
	var env_engine_http_port = getEnv(ENV_DEFAULT_ENGINE_SERVER_PORT, "")
	if env_engine_http_port != "" {
		engine_http_port, err = strconv.Atoi(env_engine_http_port)
		if err != nil {
			return nil, err
		}
	}

	// Get engine grpc port from environment or use default
	var engine_grpc_port = DEFAULT_ENGINE_GRPC_PORT
	var env_engine_grpc_port = getEnv(ENV_DEFAULT_ENGINE_SERVER_GRPC_PORT, "")
	if env_engine_grpc_port != "" {
		engine_grpc_port, err = strconv.Atoi(env_engine_grpc_port)
		if err != nil {
			return nil, err
		}
	}

	for i := 0; i < len(mlDep.Spec.Predictors); i++ {
		p := mlDep.Spec.Predictors[i]

		// Add engine deployment if separate
		_, hasSeparateEnginePod := mlDep.Spec.Annotations[machinelearningv1alpha2.ANNOTATION_SEPARATE_ENGINE]
		if hasSeparateEnginePod {
			deploy, err := createEngineDeployment(mlDep, &p, seldonId, engine_http_port, engine_grpc_port)
			if err != nil {
				return nil, err
			}
			c.deployments = append(c.deployments, deploy)
		}

		for j := 0; j < len(p.ComponentSpecs); j++ {
			cSpec := mlDep.Spec.Predictors[i].ComponentSpecs[j]
			// create Deployment from podspec
			depName := machinelearningv1alpha2.GetDeploymentName(mlDep, p, cSpec)
			deploy := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      depName,
					Namespace: namespace,
					Labels:    map[string]string{machinelearningv1alpha2.Label_seldon_id: seldonId, "app": depName},
				},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{machinelearningv1alpha2.Label_seldon_id: seldonId},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels:      map[string]string{machinelearningv1alpha2.Label_seldon_id: seldonId, "app": depName},
							Annotations: mlDep.Spec.Annotations,
						},
						Spec: cSpec.Spec,
					},
					Strategy: appsv1.DeploymentStrategy{RollingUpdate: &appsv1.RollingUpdateDeployment{MaxUnavailable: &intstr.IntOrString{StrVal: "10%"}}},
				},
			}

			// add more annotations
			for k, v := range cSpec.Metadata.Annotations {
				deploy.Spec.Template.ObjectMeta.Annotations[k] = v
			}

			// Add HPA if needed
			if cSpec.HpaSpec != nil {
				c.hpas = append(c.hpas, createHpa(cSpec, depName, seldonId, namespace))
			} else {
				deploy.Spec.Replicas = &p.Replicas
			}

			// Add service orchestrator to first deployment if needed
			if j == 0 && !hasSeparateEnginePod {
				engineContainer, err := createEngineContainer(mlDep, &p, engine_http_port, engine_grpc_port)
				if err != nil {
					return nil, err
				}
				deploy.Labels[machinelearningv1alpha2.Label_svc_orch] = "true"

				deploy.Spec.Template.Spec.Containers = append(deploy.Spec.Template.Spec.Containers, *engineContainer)
				//deploy.Spec.Template.Spec.ServiceAccountName = getEnv("ENGINE_CONTAINER_SERVICE_ACCOUNT_NAME", "seldon")
				//deploy.Spec.Template.Spec.DeprecatedServiceAccount = deploy.Spec.Template.Spec.ServiceAccountName
				//deploy.Spec.Template.Annotations = map[string]string{}
				if deploy.Spec.Template.Annotations == nil {
					deploy.Spec.Template.Annotations = make(map[string]string)
				}
				//overwrite annotations with predictor annotations
				for _, ann := range p.Annotations {
					deploy.Spec.Template.Annotations[ann] = p.Annotations[ann]
				}
				// Add prometheus annotations
				deploy.Spec.Template.Annotations["prometheus.io/path"] = getEnv("ENGINE_PROMETHEUS_PATH", "/prometheus")
				deploy.Spec.Template.Annotations["prometheus.io/port"] = strconv.Itoa(engine_http_port)
				deploy.Spec.Template.Annotations["prometheus.io/scrape"] = "true"

				deploy.ObjectMeta.Labels[machinelearningv1alpha2.Label_seldon_app] = seldonId
				deploy.Spec.Selector.MatchLabels[machinelearningv1alpha2.Label_seldon_app] = seldonId
				deploy.Spec.Template.ObjectMeta.Labels[machinelearningv1alpha2.Label_seldon_app] = seldonId

				// add predictor labels
				for k, v := range p.Labels {
					deploy.ObjectMeta.Labels[k] = v
					deploy.Spec.Template.ObjectMeta.Labels[k] = v
				}
			}

			// create services for each container
			for k := 0; k < len(cSpec.Spec.Containers); k++ {
				con := cSpec.Spec.Containers[0]
				containerServiceKey := machinelearningv1alpha2.GetPredictorServiceNameKey(&con)
				containerServiceValue := machinelearningv1alpha2.GetContainerServiceName(mlDep, p, &con)
				pu := machinelearningv1alpha2.GetPredcitiveUnit(p.Graph, con.Name)
				var portType string
				if pu.Endpoint.Type == machinelearningv1alpha2.REST {
					portType = "http"
					c.serviceDetails[containerServiceValue] = &machinelearningv1alpha2.ServiceStatus{
						SvcName:      containerServiceValue,
						HttpEndpoint: containerServiceValue + "." + namespace + ":" + strconv.Itoa(int(pu.Endpoint.ServicePort))}
				} else {
					portType = "grpc"
					c.serviceDetails[containerServiceValue] = &machinelearningv1alpha2.ServiceStatus{
						SvcName:      containerServiceValue,
						GrpcEndpoint: containerServiceValue + "." + namespace + ":" + strconv.Itoa(int(pu.Endpoint.ServicePort))}
				}
				svc := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      containerServiceValue,
						Namespace: namespace,
						Labels:    map[string]string{containerServiceKey: containerServiceValue, machinelearningv1alpha2.Label_seldon_id: mlDep.Spec.Name},
					},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{
							{
								Protocol:   corev1.ProtocolTCP,
								Port:       pu.Endpoint.ServicePort,
								TargetPort: intstr.FromInt(int(pu.Endpoint.ServicePort)),
								Name:       portType,
							},
						},
						Type:            corev1.ServiceTypeClusterIP,
						Selector:        map[string]string{containerServiceKey: containerServiceValue},
						SessionAffinity: corev1.ServiceAffinityNone,
					},
				}

				//Add labels for this service to deployment
				deploy.ObjectMeta.Labels[containerServiceKey] = containerServiceValue
				deploy.Spec.Selector.MatchLabels[containerServiceKey] = containerServiceValue
				deploy.Spec.Template.ObjectMeta.Labels[containerServiceKey] = containerServiceValue

				c.deployments = append(c.deployments, deploy)
				c.services = append(c.services, svc)
			}
		}
	}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      seldonId,
			Namespace: namespace,
			Labels: map[string]string{machinelearningv1alpha2.Label_seldon_app: seldonId,
				machinelearningv1alpha2.Label_seldon_id: seldonId},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Protocol: corev1.ProtocolTCP, Port: int32(engine_http_port), TargetPort: intstr.FromInt(engine_http_port), Name: "http"},
				{Protocol: corev1.ProtocolTCP, Port: int32(engine_grpc_port), TargetPort: intstr.FromInt(engine_grpc_port), Name: "grpc"},
			},
			Selector:        map[string]string{machinelearningv1alpha2.Label_seldon_app: seldonId},
			SessionAffinity: corev1.ServiceAffinityNone,
			Type:            corev1.ServiceTypeClusterIP,
		},
	}

	if getEnv("AMBASSADOR_ENABLED", "false") == "true" {
		svc.Annotations = make(map[string]string)
		//Create top level Service
		ambassadorConfig, err := getAmbassadorConfigs(mlDep, seldonId, engine_http_port, engine_grpc_port)
		if err != nil {
			return nil, err
		}
		svc.Annotations["getambassador.io/config"] = ambassadorConfig
	}

	if getAnnotation(mlDep, machinelearningv1alpha2.ANNOTATION_HEADLESS_SVC, "false") != "false" {
		log.Info("Creating Headless SVC")
		svc.Spec.ClusterIP = "None"
	}
	c.services = append(c.services, svc)
	c.serviceDetails[seldonId] = &machinelearningv1alpha2.ServiceStatus{
		SvcName:      seldonId,
		HttpEndpoint: seldonId + "." + namespace + ":" + strconv.Itoa(engine_http_port),
		GrpcEndpoint: seldonId + "." + namespace + ":" + strconv.Itoa(engine_grpc_port),
	}

	if getEnv(ENV_ISTIO_ENABLED, "false") == "true" {
		vsvcs, dstRule := createIstioResources(mlDep, seldonId, namespace, engine_http_port, engine_grpc_port)
		c.virtualService = vsvcs
		c.destinationRule = dstRule
	}

	return &c, nil
}

// Create Services specified in components.
func createIstioServices(r *ReconcileSeldonDeployment, components *components, instance *machinelearningv1alpha2.SeldonDeployment) (bool, error) {
	ready := true
	for _, svc := range components.virtualService {
		if err := controllerutil.SetControllerReference(instance, svc, r.scheme); err != nil {
			return ready, err
		}
		found := &istio.VirtualService{}
		err := r.Get(context.TODO(), types.NamespacedName{Name: svc.Name, Namespace: svc.Namespace}, found)
		if err != nil && errors.IsNotFound(err) {
			ready = false
			log.Info("Creating Virtual Service", "namespace", svc.Namespace, "name", svc.Name)
			err = r.Create(context.TODO(), svc)
			if err != nil {
				return ready, err
			}

		} else if err != nil {
			return ready, err
		} else {
			// Update the found object and write the result back if there are any changes
			if !reflect.DeepEqual(svc.Spec, found.Spec) {
				ready = false
				found.Spec = svc.Spec
				log.Info("Updating Virtual Service", "namespace", svc.Namespace, "name", svc.Name)
				err = r.Update(context.TODO(), found)
				if err != nil {
					return ready, err
				}
			} else {
				log.Info("Found identical Virtual Service", "namespace", found.Namespace, "name", found.Name)

				if instance.Status.ServiceStatus == nil {
					instance.Status.ServiceStatus = map[string]machinelearningv1alpha2.ServiceStatus{}
				}

				/*
					if _, ok := instance.Status.ServiceStatus[found.Name]; !ok {
						instance.Status.ServiceStatus[found.Name] = *components.serviceDetails[found.Spec.HTTP[0].Route[0].Destination.Host]
						err = r.Status().Update(context.Background(), instance)
						if err != nil {
							return ready, err
						}
					}
				*/
			}
		}

	}

	if components.destinationRule != nil {
		drule := components.destinationRule
		if err := controllerutil.SetControllerReference(instance, drule, r.scheme); err != nil {
			return ready, err
		}
		found := &istio.DestinationRule{}
		err := r.Get(context.TODO(), types.NamespacedName{Name: drule.Name, Namespace: drule.Namespace}, found)
		if err != nil && errors.IsNotFound(err) {
			ready = false
			log.Info("Creating Istio Destination Rule", "namespace", drule.Namespace, "name", drule.Name)
			err = r.Create(context.TODO(), drule)
			if err != nil {
				return ready, err
			}

		} else if err != nil {
			return ready, err
		} else {
			// Update the found object and write the result back if there are any changes
			if !reflect.DeepEqual(drule.Spec, found.Spec) {
				ready = false
				found.Spec = drule.Spec
				log.Info("Updating Istio Destination Rule", "namespace", drule.Namespace, "name", drule.Name)
				err = r.Update(context.TODO(), found)
				if err != nil {
					return ready, err
				}
			} else {
				log.Info("Found identical Istio Destination Rule", "namespace", found.Namespace, "name", found.Name)

				if instance.Status.ServiceStatus == nil {
					instance.Status.ServiceStatus = map[string]machinelearningv1alpha2.ServiceStatus{}
				}

				if _, ok := instance.Status.ServiceStatus[found.Name]; !ok {
					instance.Status.ServiceStatus[found.Name] = *components.serviceDetails[found.Name]
					err = r.Status().Update(context.Background(), instance)
					if err != nil {
						return ready, err
					}
				}
			}
		}

	}

	return ready, nil
}

// Create Services specified in components.
func createServices(r *ReconcileSeldonDeployment, components *components, instance *machinelearningv1alpha2.SeldonDeployment) (bool, error) {
	ready := true
	for _, svc := range components.services {
		if err := controllerutil.SetControllerReference(instance, svc, r.scheme); err != nil {
			return ready, err
		}
		found := &corev1.Service{}
		err := r.Get(context.TODO(), types.NamespacedName{Name: svc.Name, Namespace: svc.Namespace}, found)
		if err != nil && errors.IsNotFound(err) {
			ready = false
			log.Info("Creating Service", "namespace", svc.Namespace, "name", svc.Name)
			err = r.Create(context.TODO(), svc)
			if err != nil {
				return ready, err
			}

		} else if err != nil {
			return ready, err
		} else {
			svc.Spec.ClusterIP = found.Spec.ClusterIP
			// Update the found object and write the result back if there are any changes
			if !reflect.DeepEqual(svc.Spec, found.Spec) {
				ready = false
				found.Spec = svc.Spec
				log.Info("Updating Service", "namespace", svc.Namespace, "name", svc.Name)
				err = r.Update(context.TODO(), found)
				if err != nil {
					return ready, err
				}
			} else {
				log.Info("Found identical Service", "namespace", found.Namespace, "name", found.Name, "status", found.Status)

				if instance.Status.ServiceStatus == nil {
					instance.Status.ServiceStatus = map[string]machinelearningv1alpha2.ServiceStatus{}
				}

				if _, ok := instance.Status.ServiceStatus[found.Name]; !ok {
					instance.Status.ServiceStatus[found.Name] = *components.serviceDetails[found.Name]
					err = r.Status().Update(context.Background(), instance)
					if err != nil {
						return ready, err
					}
				}
			}
		}

	}

	return ready, nil
}

// Create Services specified in components.
func createHpas(r *ReconcileSeldonDeployment, components *components, instance *machinelearningv1alpha2.SeldonDeployment) (bool, error) {
	ready := true
	for _, hpa := range components.hpas {
		if err := controllerutil.SetControllerReference(instance, hpa, r.scheme); err != nil {
			return ready, err
		}
		found := &autoscaling.HorizontalPodAutoscaler{}
		err := r.Get(context.TODO(), types.NamespacedName{Name: hpa.Name, Namespace: hpa.Namespace}, found)
		if err != nil && errors.IsNotFound(err) {
			ready = false
			log.Info("Creating HPA", "namespace", hpa.Namespace, "name", hpa.Name)
			err = r.Create(context.TODO(), hpa)
			if err != nil {
				return ready, err
			}

		} else if err != nil {
			return ready, err
		} else {
			// Update the found object and write the result back if there are any changes
			if !reflect.DeepEqual(hpa.Spec, found.Spec) {
				found.Spec = hpa.Spec
				ready = false
				log.Info("Updating HPA", "namespace", hpa.Namespace, "name", hpa.Name)
				err = r.Update(context.TODO(), found)
				if err != nil {
					return ready, err
				}
			} else {
				log.Info("Found identical HPA", "namespace", found.Namespace, "name", found.Name, "status", found.Status)
			}
		}

	}
	return ready, nil
}

func jsonEquals(a, b interface{}) (bool, error) {
	b1, err := json.Marshal(a)
	if err != nil {
		return false, err
	}
	b2, err := json.Marshal(b)
	if err != nil {
		return false, err
	}
	return bytes.Equal(b1, b2), nil
}

// Create Deployments specified in components.
func createDeployments(r *ReconcileSeldonDeployment, components *components, instance *machinelearningv1alpha2.SeldonDeployment) (bool, error) {
	ready := true
	for _, deploy := range components.deployments {

		if err := controllerutil.SetControllerReference(instance, deploy, r.scheme); err != nil {
			return ready, err
		}

		// TODO(user): Change this for the object type created by your controller
		// Check if the Deployment already exists
		found := &appsv1.Deployment{}
		err := r.Get(context.TODO(), types.NamespacedName{Name: deploy.Name, Namespace: deploy.Namespace}, found)
		if err != nil && errors.IsNotFound(err) {
			ready = false
			log.Info("Creating Deployment", "namespace", deploy.Namespace, "name", deploy.Name)
			err = r.Create(context.TODO(), deploy)
			if err != nil {
				return ready, err
			}

		} else if err != nil {
			return ready, err
		} else {
			//Hack to add default procMount which if not present in old k8s versions might cause us to believe the Specs are different and we need an update
			for _, c := range found.Spec.Template.Spec.Containers {
				if c.SecurityContext != nil && c.SecurityContext.ProcMount != nil {
					var procMount = corev1.DefaultProcMount
					c.SecurityContext.ProcMount = &procMount
				}
			}
			// Update the found object and write the result back if there are any changes
			jEquals, err := jsonEquals(deploy.Spec.Template.Spec, found.Spec.Template.Spec)
			if err != nil {
				return ready, err
			}
			//if !reflect.DeepEqual(deploy.Spec.Template.Spec, found.Spec.Template.Spec) {
			if !jEquals {
				log.Info("Updating Deployment", "namespace", deploy.Namespace, "name", deploy.Name)

				jStr1, err := json.Marshal(deploy.Spec.Template.Spec)
				fmt.Println(string(jStr1))
				jStr2, err := json.Marshal(found.Spec.Template.Spec)
				fmt.Println(string(jStr2))

				if !reflect.DeepEqual(deploy.Spec.Template.Spec.Containers[0].Resources, found.Spec.Template.Spec.Containers[0].Resources) {
					log.Info("Containers differ")
				}

				ready = false
				found.Spec = deploy.Spec

				err = r.Update(context.TODO(), found)
				if err != nil {
					return ready, err
				}
			} else {
				log.Info("Found identical deployment", "namespace", found.Namespace, "name", found.Name, "status", found.Status)
				deploymentStatus, present := instance.Status.DeploymentStatus[found.Name]

				if !present {
					deploymentStatus = machinelearningv1alpha2.DeploymentStatus{}
				}

				if deploymentStatus.Replicas != found.Status.Replicas || deploymentStatus.AvailableReplicas != found.Status.AvailableReplicas {
					deploymentStatus.Replicas = found.Status.Replicas
					deploymentStatus.AvailableReplicas = found.Status.AvailableReplicas
					if instance.Status.DeploymentStatus == nil {
						instance.Status.DeploymentStatus = map[string]machinelearningv1alpha2.DeploymentStatus{}
					}

					instance.Status.DeploymentStatus[found.Name] = deploymentStatus

					err = r.Status().Update(context.Background(), instance)
					if err != nil {
						return ready, err
					}
				}
				log.Info("Deployment status ", "name", found.Name, "status", found.Status)
				if found.Status.ReadyReplicas == 0 || found.Status.UnavailableReplicas > 0 {
					ready = false
				}
			}
		}
	}

	// Clean up any old deployments and services
	// 1. Delete any svc-orchestroator deployments
	// 2. Delete any other deployments
	// 3. Delete any services
	// Deletion is done in foreground so we wait for underlying pods to be removed
	if ready {
		statusCopy := instance.Status.DeepCopy()
		//delete from copied status the current expected deployments by name
		for _, deploy := range components.deployments {
			delete(statusCopy.DeploymentStatus, deploy.Name)
		}
		for k := range components.serviceDetails {
			delete(statusCopy.ServiceStatus, k)
		}
		remaining := len(statusCopy.DeploymentStatus)
		// Any deployments left in status should be removed as they are not part of the current graph
		svcOrchExists := false
		for k := range statusCopy.DeploymentStatus {
			found := &appsv1.Deployment{}
			err := r.Get(context.TODO(), types.NamespacedName{Name: k, Namespace: instance.Namespace}, found)
			if err != nil && errors.IsNotFound(err) {

			} else {
				if _, ok := found.ObjectMeta.Labels[machinelearningv1alpha2.Label_svc_orch]; ok {
					log.Info("Found existing svc-orch")
					svcOrchExists = true
					break
				}
			}
		}
		for k := range statusCopy.DeploymentStatus {
			found := &appsv1.Deployment{}
			err := r.Get(context.TODO(), types.NamespacedName{Name: k, Namespace: instance.Namespace}, found)
			if err != nil && errors.IsNotFound(err) {
				log.Info("Failed to find old deployment - removing from status", "name", k)
				// clean up status
				delete(instance.Status.DeploymentStatus, k)
				err = r.Status().Update(context.Background(), instance)
				if err != nil {
					return ready, err
				}
				return ready, err
			} else {
				if svcOrchExists {
					if _, ok := found.ObjectMeta.Labels[machinelearningv1alpha2.Label_svc_orch]; ok {
						log.Info("Deleting old svc-orch deployment ", "name", k)

						err := r.Delete(context.TODO(), found, client.PropagationPolicy(metav1.DeletePropagationForeground))
						if err != nil {
							return ready, err
						}
					}
				} else {
					log.Info("Deleting old deployment (svc-orch does not exist)", "name", k)

					err := r.Delete(context.TODO(), found, client.PropagationPolicy(metav1.DeletePropagationForeground))
					if err != nil {
						return ready, err
					}
				}
			}
		}
		if remaining == 0 {
			log.Info("Removing unused services")
			for k := range statusCopy.ServiceStatus {
				found := &corev1.Service{}
				err := r.Get(context.TODO(), types.NamespacedName{Name: k, Namespace: instance.Namespace}, found)
				if err != nil && errors.IsNotFound(err) {
					log.Error(err, "Failed to find old service", "name", k)
					return ready, err
				} else {
					log.Info("Deleting old service ", "name", k)
					// clean up status
					delete(instance.Status.ServiceStatus, k)
					err = r.Status().Update(context.Background(), instance)
					if err != nil {
						return ready, err
					}
					err := r.Delete(context.TODO(), found)
					if err != nil {
						return ready, err
					}
				}
			}
		}
	}
	return ready, nil
}

// Reconcile reads that state of the cluster for a SeldonDeployment object and makes changes based on the state read
// and what is in the SeldonDeployment.Spec
// TODO(user): Modify this Reconcile function to implement your Controller logic.  The scaffolding writes
// a Deployment as an example
// Automatically generate RBAC rules to allow the Controller to read and write Deployments
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=v1,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=v1,resources=services/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=networking.istio.io,resources=virtualservices,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.istio.io,resources=virtualservices/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=networking.istio.io,resources=destinationrules,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.istio.io,resources=destinationrules/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=autoscaling,resources=horizontalpodautoscalers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=autoscaling,resources=horizontalpodautoscalers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=machinelearning.seldon.io,resources=seldondeployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=machinelearning.seldon.io,resources=seldondeployments/status,verbs=get;update;patch
func (r *ReconcileSeldonDeployment) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	// Fetch the SeldonDeployment instance
	instance := &machinelearningv1alpha2.SeldonDeployment{}
	err := r.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Object not found, return.  Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	components, err := createComponents(instance)
	if err != nil {
		return reconcile.Result{}, err
	}

	deploymentsReady, err := createDeployments(r, components, instance)
	if err != nil {
		return reconcile.Result{}, err
	}

	servicesReady, err := createServices(r, components, instance)
	if err != nil {
		return reconcile.Result{}, err
	}

	virtualServicesReady, err := createIstioServices(r, components, instance)
	if err != nil {
		return reconcile.Result{}, err
	}

	hpasReady, err := createHpas(r, components, instance)
	if err != nil {
		return reconcile.Result{}, err
	}

	if deploymentsReady && servicesReady && hpasReady && virtualServicesReady {
		instance.Status.State = "Available"
	} else {
		instance.Status.State = "Creating"
	}
	err = r.Status().Update(context.Background(), instance)
	if err != nil {
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

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
	"context"
	"encoding/base64"
	"encoding/json"
	machinelearningv1alpha2 "github.com/seldonio/seldon-operator/pkg/apis/machinelearning/v1alpha2"
	appsv1 "k8s.io/api/apps/v1"
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
	Label_seldon_id  = "seldon-deployment-id"
	Label_seldon_app = "seldon-app"
)

var log = logf.Log.WithName("controller")

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

	// TODO(user): Modify this to be the types you create
	// Uncomment watch a Deployment created by SeldonDeployment - change this for objects you create
	err = c.Watch(&source.Kind{Type: &appsv1.Deployment{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &machinelearningv1alpha2.SeldonDeployment{},
	})
	if err != nil {
		return err
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
	deployments []*appsv1.Deployment
	services    []*corev1.Service
}

func getNamespace(deployment *machinelearningv1alpha2.SeldonDeployment) string {
	if len(deployment.ObjectMeta.Namespace) > 0 {
		return deployment.ObjectMeta.Namespace
	} else {
		return "default"
	}
}

const (
	PODINFO_VOLUME_NAME = "podInfo"
	PODINFO_VOLUME_PATH = "/etc/podinfo"

	ANNOTATION_JAVA_OPTS = "seldonio/engine-java-opts"
	ANNOTATION_SEPARATE_ENGINE = "seldon.io/engine-separate-pod"

	DEFAULT_ENGINE_CONTAINER_PORT = 8000
	DEFAULT_ENGINE_GRPC_PORT = 5001
)

func getEngineVarJson(p *machinelearningv1alpha2.PredictorSpec) (string, error) {
	str, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	return  base64.StdEncoding.EncodeToString(str), nil
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func createEngineContainer(mlDep *machinelearningv1alpha2.SeldonDeployment, p *machinelearningv1alpha2.PredictorSpec) (*corev1.Container,error) {
	var engineUser int64 = 8888
	// get predictor as base64 encoded json
	predictorB64, err := getEngineVarJson(p)
	if err != nil {
		return nil, err
	}

	//get annotation for java opts or default
	javaOpts, err := mlDep.Spec.Annotations[ANNOTATION_JAVA_OPTS]
	if err != nil {
		javaOpts = "-Dcom.sun.management.jmxremote.rmi.port=9090 -Dcom.sun.management.jmxremote -Dcom.sun.management.jmxremote.port=9090 -Dcom.sun.management.jmxremote.ssl=false -Dcom.sun.management.jmxremote.authenticate=false -Dcom.sun.management.jmxremote.local.only=false -Djava.rmi.server.hostname=127.0.0.1"
	}


	//Engine resources
	engineResources :=  p.SvcOrchSpec.Resources
	if engineResources == nil {
		cpuQuantity,_  := resource.ParseQuantity("1")
		engineResources = &corev1.ResourceRequirements{
			Requests: map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceCPU:cpuQuantity,
			},
		}
	}

	c := corev1.Container{
			Name:            "seldon-container-engine",
			Image:           getEnv("ENGINE_CONTAINER_IMAGE_AND_VERSION","seldonio/engine:0.2.7-SNAPSHOT"),
			ImagePullPolicy: corev1.PullPolicy(getEnv("ENGINE_CONTAINER_IMAGE_PULL_POLICY","IfNotPresent")),
			VolumeMounts: []corev1.VolumeMount{
				{
					Name:      PODINFO_VOLUME_NAME,
					MountPath: PODINFO_VOLUME_PATH,
				},
			},
			Env: []corev1.EnvVar{
				{Name: "ENGINE_PREDICTOR", Value: predictorB64},
				{Name: "DEPLOYMENT_NAME", Value: mlDep.Spec.Name},
				{Name: "ENGINE_SERVER_PORT", Value: strconv.Itoa(DEFAULT_ENGINE_CONTAINER_PORT)},
				{Name: "ENGINE_SERVER_GRPC_PORT", Value: strconv.Itoa(DEFAULT_ENGINE_GRPC_PORT)},
				{Name: "JAVA_OPTS", Value: javaOpts},
			},
			Ports: []corev1.ContainerPort{
				{ContainerPort: DEFAULT_ENGINE_CONTAINER_PORT},
				{ContainerPort: DEFAULT_ENGINE_GRPC_PORT},
				{ContainerPort: 8082, Name: "admin"},
				{ContainerPort: 9090, Name: "jmx"},
			},
			SecurityContext: &corev1.SecurityContext{RunAsUser: &engineUser},
			ReadinessProbe: &corev1.Probe{Handler: corev1.Handler{HTTPGet: &corev1.HTTPGetAction{Port: intstr.IntOrString{Type: 1, StrVal: "admin"}, Path: "/ready"}},
				InitialDelaySeconds: 20,
				PeriodSeconds:       1,
				FailureThreshold:    1,
				SuccessThreshold:    1,
				TimeoutSeconds:      2},
			LivenessProbe:&corev1.Probe{Handler: corev1.Handler{HTTPGet: &corev1.HTTPGetAction{Port: intstr.IntOrString{Type: 1, StrVal: "admin"}, Path: "/ready"}},
				InitialDelaySeconds: 20,
				PeriodSeconds:       5,
				FailureThreshold:    3,
				SuccessThreshold:    1,
				TimeoutSeconds:      2},
			Lifecycle: &corev1.Lifecycle{
				PreStop: &corev1.Handler{
					Exec: &corev1.ExecAction{Command: []string{"/bin/sh","-c","curl 127.0.0.1:"+strconv.Itoa(DEFAULT_ENGINE_GRPC_PORT)+"/pause && /bin/sleep 10"}},
				},
			},
			Resources:*engineResources,
		}

	return &c, nil
}

func createEngineDeployment(mlDep *machinelearningv1alpha2.SeldonDeployment, p *machinelearningv1alpha2.PredictorSpec,seldonId string) (*appsv1.Deployment, error)  {

	var terminationGracePeriodSecs = int64(20)

	depName := GetServiceOrchestratorName(mlDep,p)

	con, err := createEngineContainer(mlDep,p)
	if err != nil {
		return nil, err
	}
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:        depName,
			Namespace:   getNamespace(mlDep),
			Labels:      map[string]string{Label_seldon_id: seldonId, "app": depName,"version":"v1"},
			Annotations: p.Annotations,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{Label_seldon_id: seldonId},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{Label_seldon_id: seldonId, "app": depName},
					Annotations: map[string]string{
						"prometheus.io/path":"/prometheus",
						"prometheus.io/port":strconv.Itoa(DEFAULT_ENGINE_CONTAINER_PORT),
						"prometheus.io/scrape":"true",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						*con,
					},
					TerminationGracePeriodSeconds:&terminationGracePeriodSecs,
					ServiceAccountName:getEnv("ENGINE_CONTAINER_SERVICE_ACCOUNT_NAME","seldon"),
					Volumes: []corev1.Volume{
						{Name:PODINFO_VOLUME_NAME,VolumeSource: corev1.VolumeSource{DownwardAPI: &corev1.DownwardAPIVolumeSource{Items:[]corev1.DownwardAPIVolumeFile{
							{Path:"annotations",FieldRef: &corev1.ObjectFieldSelector{FieldPath:"metadata.annotations"}},
						}}}},
					},
				},
			},
			Strategy: appsv1.DeploymentStrategy{RollingUpdate: &appsv1.RollingUpdateDeployment{MaxUnavailable:&intstr.IntOrString{StrVal:"10%"}}},
			Replicas:&p.Replicas,
		},
	}
	return deploy, nil
}

func createComponents(mlDep *machinelearningv1alpha2.SeldonDeployment) (*components, error) {
	c := components{}
	seldonId := GetSeldonDeploymentName(mlDep)

	for i := 0; i < len(mlDep.Spec.Predictors); i++ {
		p := mlDep.Spec.Predictors[i]

		// Add engine deployment if separate
		_, hasSeparateEnginePod := mlDep.Spec.Annotations[ANNOTATION_SEPARATE_ENGINE]
		if hasSeparateEnginePod {
			deploy, err := createEngineDeployment(mlDep,&p,seldonId)
			if err != nil {
				return nil, err
			}
			c.deployments = append(c.deployments, deploy)
		}

		for j := 0; j < len(p.ComponentSpecs); j++ {
			cSpec := mlDep.Spec.Predictors[i].ComponentSpecs[j]
			// create Deployment from podspec
			depName := GetDeploymentName(mlDep, p, cSpec)
			deploy := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:        depName,
					Namespace:   getNamespace(mlDep),
					Labels:      map[string]string{Label_seldon_id: seldonId, "app": depName},
					Annotations: p.Annotations,
				},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{Label_seldon_id: seldonId},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{Label_seldon_id: seldonId, "app": depName},
						},
						Spec: cSpec.Spec,
					},
				},

			}
			c.deployments = append(c.deployments, deploy)

			// create services for each container
			for k := 0; k < len(cSpec.Spec.Containers); k++ {
				con := cSpec.Spec.Containers[0]
				containerServiceKey := GetPredictorServiceNameKey(&con)
				containerServiceValue := GetContainerServiceName(mlDep, p, &con)
				svc := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:   containerServiceValue,
						Labels: map[string]string{containerServiceKey: containerServiceValue, Label_seldon_id: mlDep.Spec.Name},
					},
				}
				c.services = append(c.services, svc)
			}
		}
	}
	return &c, nil
}

func createDeployments(r *ReconcileSeldonDeployment, components *components, instance *machinelearningv1alpha2.SeldonDeployment) error {
	for _, deploy := range components.deployments {

		if err := controllerutil.SetControllerReference(instance, deploy, r.scheme); err != nil {
			return err
		}

		// TODO(user): Change this for the object type created by your controller
		// Check if the Deployment already exists
		found := &appsv1.Deployment{}
		err := r.Get(context.TODO(), types.NamespacedName{Name: deploy.Name, Namespace: deploy.Namespace}, found)
		if err != nil && errors.IsNotFound(err) {
			log.Info("Creating Deployment", "namespace", deploy.Namespace, "name", deploy.Name)
			err = r.Create(context.TODO(), deploy)
			if err != nil {
				return err
			}
			//instance.Status.State = "Creating"
			//err = r.Status().Update(context.Background(), instance)
			//if err != nil {
			//	return err
			//}

		} else if err != nil {
			return err
		}

		// TODO(user): Change this for the object type created by your controller
		// Update the found object and write the result back if there are any changes
		if !reflect.DeepEqual(deploy.Spec, found.Spec) {
			found.Spec = deploy.Spec
			log.Info("Updating Deployment", "namespace", deploy.Namespace, "name", deploy.Name)
			err = r.Update(context.TODO(), found)
			if err != nil {
				return err
			}
		} else {
			log.Info("Found identical deployment", "namespace", found.Namespace, "name", found.Name, "status", found.Status)
		}

	}
	return nil
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

	components, _ := createComponents(instance)

	err = createDeployments(r, components, instance)
	if err != nil {
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

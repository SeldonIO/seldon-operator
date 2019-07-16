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

package mutating

import (
	"context"
	"encoding/json"
	"github.com/seldonio/seldon-operator/pkg/utils"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"net/http"
	"os"
	"strconv"
	"fmt"

	machinelearningv1alpha2 "github.com/seldonio/seldon-operator/pkg/apis/machinelearning/v1alpha2"
	"github.com/seldonio/seldon-operator/pkg/constants"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/runtime/inject"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission/types"
)

var (
	DefaultSKLearnServerImageNameRest = "seldonio/sklearnserver_rest:0.1"
	DefaultSKLearnServerImageNameGrpc = "seldonio/sklearnserver_grpc:0.1"
	DefaultXGBoostServerImageNameRest = "seldonio/xgboostserver_rest:0.1"
	DefaultXGBoostServerImageNameGrpc = "seldonio/xgboostserver_grpc:0.1"
	DefaultTFServerImageNameRest = "seldonio/tfserving-proxy_rest:0.3"
	DefaultTFServerImageNameGrpc = "seldonio/tfserving-proxy_grpc:0.3"
)

func init() {
	webhookName := "mutating-create-update-seldondeployment"
	if HandlerMap[webhookName] == nil {
		HandlerMap[webhookName] = []admission.Handler{}
	}
	HandlerMap[webhookName] = append(HandlerMap[webhookName], &SeldonDeploymentCreateUpdateHandler{})
}

// SeldonDeploymentCreateUpdateHandler handles SeldonDeployment
type SeldonDeploymentCreateUpdateHandler struct {
	// To use the client, you need to do the following:
	// - uncomment it
	// - import sigs.k8s.io/controller-runtime/pkg/client
	// - uncomment the InjectClient method at the bottom of this file.
	// Client  client.Client

	// Decoder decodes objects
	Decoder types.Decoder
}

func getPort(name string, ports []v1.ContainerPort) *v1.ContainerPort {
	for i := 0; i < len(ports); i++ {
		if ports[i].Name == name {
			return &ports[i]
		}
	}
	return nil
}

func getPredictiveUnitAsJson(params []machinelearningv1alpha2.Parameter) string {
	str, err := json.Marshal(params)
	if err != nil {
		return ""
	} else {
		return string(str)
	}
}

func addDefaultsToGraph(pu *machinelearningv1alpha2.PredictiveUnit) {
	if pu.Type == nil {
		ty := machinelearningv1alpha2.UNKNOWN_TYPE
		pu.Type = &ty
	}
	if pu.Implementation == nil {
		im := machinelearningv1alpha2.UNKNOWN_IMPLEMENTATION
		pu.Implementation = &im
	}
	for i := 0; i < len(pu.Children); i++ {
		addDefaultsToGraph(&pu.Children[i])
	}
}

func addTFServerContainer(pu *machinelearningv1alpha2.PredictiveUnit, p *machinelearningv1alpha2.PredictorSpec) error {

	if *pu.Implementation == machinelearningv1alpha2.TENSORFLOW_SERVER {

		ty := machinelearningv1alpha2.MODEL
		pu.Type = &ty

		if pu.Endpoint == nil {
			pu.Endpoint = &machinelearningv1alpha2.Endpoint{Type: machinelearningv1alpha2.REST}
		}

		c := utils.GetContainerForPredictiveUnit(p, pu.Name)
		existing := c != nil
		if !existing {
			c = &v1.Container{
				Name: pu.Name,
			}
		}

		var uriParam machinelearningv1alpha2.Parameter
		//Add missing fields
		// Add image
		if c.Image == "" {
			if pu.Endpoint.Type == machinelearningv1alpha2.REST {
				c.Image = DefaultTFServerImageNameRest
				uriParam = machinelearningv1alpha2.Parameter{
					Name:  "rest_endpoint",
					Type:  "STRING",
					Value: "http://0.0.0.0:2001",
				}
			} else {
				c.Image = DefaultTFServerImageNameGrpc
				uriParam = machinelearningv1alpha2.Parameter{
					Name:  "grpc_endpoint",
					Type:  "STRING",
					Value: "0.0.0.0:2000",
				}

			}
			c.ImagePullPolicy = v1.PullAlways
		}

		// Add parameters
		if pu.Parameters == nil {
			pu.Parameters = []machinelearningv1alpha2.Parameter{}
		}
		pu.Parameters = append(pu.Parameters,uriParam)

		// Add container to componentSpecs
		if !existing {
			if len(p.ComponentSpecs) > 0 {
				p.ComponentSpecs[0].Spec.Containers = append(p.ComponentSpecs[0].Spec.Containers, *c)
			} else {
				podSpec := machinelearningv1alpha2.SeldonPodSpec{
					Metadata: metav1.ObjectMeta{CreationTimestamp: metav1.Now()},
					Spec: v1.PodSpec{
						Containers: []v1.Container{*c},
					},
				}
				p.ComponentSpecs = []*machinelearningv1alpha2.SeldonPodSpec{&podSpec}
			}
		}

		tfServingContainer := v1.Container{
			Name:  "tfserving",
			Image: "tensorflow/serving:latest",
			Args: []string{
				"/usr/bin/tensorflow_model_server",
				"--port=2000",
				"--rest_api_port=2001",
				"--model_name="+pu.Name,
				"--model_base_path=" + pu.ModelURI},
			ImagePullPolicy: v1.PullIfNotPresent,
			Ports: []v1.ContainerPort{
				{
					ContainerPort: 2000,
					Protocol: v1.ProtocolTCP,
				},
				{
					ContainerPort: 2001,
					Protocol: v1.ProtocolTCP,
				},
			},
		}
		p.ComponentSpecs[0].Spec.Containers = append(p.ComponentSpecs[0].Spec.Containers, tfServingContainer)
	}
	return nil
}

func addModelDefaultServers(pu *machinelearningv1alpha2.PredictiveUnit, p *machinelearningv1alpha2.PredictorSpec) error {
	if *pu.Implementation == machinelearningv1alpha2.SKLEARN_SERVER ||
		*pu.Implementation == machinelearningv1alpha2.XGBOOST_SERVER {

		ty := machinelearningv1alpha2.MODEL
		pu.Type = &ty

		if pu.Endpoint == nil {
			pu.Endpoint = &machinelearningv1alpha2.Endpoint{Type: machinelearningv1alpha2.REST}
		}
		c := utils.GetContainerForPredictiveUnit(p, pu.Name)
		existing := c != nil
		if !existing {
			c = &v1.Container{
				Name: pu.Name,
			}
		}

		//Add missing fields
		// Add image
		if c.Image == "" {
			if *pu.Implementation == machinelearningv1alpha2.SKLEARN_SERVER {
				if pu.Endpoint.Type == machinelearningv1alpha2.REST {
					c.Image = DefaultSKLearnServerImageNameRest
				} else {
					c.Image = DefaultSKLearnServerImageNameGrpc
				}
			} else if *pu.Implementation == machinelearningv1alpha2.XGBOOST_SERVER {
				if pu.Endpoint.Type == machinelearningv1alpha2.REST {
					c.Image = DefaultXGBoostServerImageNameRest
				} else {
					c.Image = DefaultXGBoostServerImageNameGrpc
				}
			}
		}
		// Add parameters envvar
		if !utils.HasEnvVar(c.Env, constants.PU_PARAMETER_ENVVAR) {
			params := pu.Parameters
			uriParam := machinelearningv1alpha2.Parameter{
					Name:  "model_uri",
					Type:  "STRING",
					Value: pu.ModelURI,
			}
			params = append(params,uriParam)
			paramStr, err := json.Marshal(params)
			if err != nil {
				return err
			}
			c.Env = append(c.Env, corev1.EnvVar{Name: constants.PU_PARAMETER_ENVVAR, Value: string(paramStr)})
		}

		// Add container to componentSpecs
		if !existing {
			if len(p.ComponentSpecs) > 0 {
				p.ComponentSpecs[0].Spec.Containers = append(p.ComponentSpecs[0].Spec.Containers, *c)
			} else {
				podSpec := machinelearningv1alpha2.SeldonPodSpec{
					Metadata: metav1.ObjectMeta{CreationTimestamp: metav1.Now()},
					Spec: v1.PodSpec{
						Containers: []v1.Container{*c},
					},
				}
				p.ComponentSpecs = []*machinelearningv1alpha2.SeldonPodSpec{&podSpec}
			}
		}
	}
	return nil
}

func addModelServerContainers(pu *machinelearningv1alpha2.PredictiveUnit, p *machinelearningv1alpha2.PredictorSpec) error {

	if err := addModelDefaultServers(pu,p); err != nil {
		return err
	}
	if err := addTFServerContainer(pu,p); err != nil {
		return err
	}

	for i := 0; i < len(pu.Children); i++ {
		if err := addModelServerContainers(&pu.Children[i],p) ; err != nil {
			return err
		}
	}
	return nil
}

func (h *SeldonDeploymentCreateUpdateHandler) MutatingSeldonDeploymentFn(ctx context.Context, mlDep *machinelearningv1alpha2.SeldonDeployment) error {
	var nextPortNum int32 = 9000
	var terminationGracePeriod int64 = 20
	if env_preditive_unit_service_port, ok := os.LookupEnv("PREDICTIVE_UNIT_SERVICE_PORT"); ok {
		portNum, err := strconv.Atoi(env_preditive_unit_service_port)
		if err != nil {
			return err
		} else {
			nextPortNum = int32(portNum)
		}
	}

	var defaultMode = corev1.DownwardAPIVolumeSourceDefaultMode

	portMap := map[string]int32{}
	if mlDep.ObjectMeta.Namespace == "" {
		mlDep.ObjectMeta.Namespace = "default"
	}
	for i := 0; i < len(mlDep.Spec.Predictors); i++ {
		p := mlDep.Spec.Predictors[i]
		if p.Graph.Type == nil {
			ty := machinelearningv1alpha2.UNKNOWN_TYPE
			p.Graph.Type = &ty
		}
		// Add version label for predictor if not present
		if p.Labels == nil {
			p.Labels = map[string]string{}
		}
		if _, present := p.Labels["version"]; !present {
			p.Labels["version"] = p.Name
		}
		addDefaultsToGraph(p.Graph)
		if err := addModelServerContainers(p.Graph,&p); err != nil {
			return err
		}
		fmt.Println("predictor is now")
		jstr,_ := json.Marshal(p)
		fmt.Println(string(jstr))

		mlDep.Spec.Predictors[i] = p

		for j := 0; j < len(p.ComponentSpecs); j++ {
			cSpec := mlDep.Spec.Predictors[i].ComponentSpecs[j]

			//Add some default to help with diffs in controller
			if cSpec.Spec.RestartPolicy == "" {
				cSpec.Spec.RestartPolicy = corev1.RestartPolicyAlways
			}
			if cSpec.Spec.DNSPolicy == "" {
				cSpec.Spec.DNSPolicy = corev1.DNSClusterFirst
			}
			if cSpec.Spec.SchedulerName == "" {
				cSpec.Spec.SchedulerName = "default-scheduler"
			}
			if cSpec.Spec.SecurityContext == nil {
				cSpec.Spec.SecurityContext = &corev1.PodSecurityContext{}
			}

			cSpec.Spec.TerminationGracePeriodSeconds = &terminationGracePeriod

			//Add downwardAPI
			cSpec.Spec.Volumes = append(cSpec.Spec.Volumes, corev1.Volume{Name: machinelearningv1alpha2.PODINFO_VOLUME_NAME, VolumeSource: corev1.VolumeSource{
				DownwardAPI: &corev1.DownwardAPIVolumeSource{Items: []corev1.DownwardAPIVolumeFile{
					{Path: "annotations", FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.annotations", APIVersion: "v1"}}}, DefaultMode: &defaultMode}}})

			// create services for each container
			for k := 0; k < len(cSpec.Spec.Containers); k++ {
				con := &cSpec.Spec.Containers[k]

				if _, present := portMap[con.Name]; !present {
					portMap[con.Name] = nextPortNum
					nextPortNum++
				}
				portNum := portMap[con.Name]

				pu := machinelearningv1alpha2.GetPredcitiveUnit(p.Graph, con.Name)

				if pu != nil {

					// Add some defaults for easier diffs later in controller
					if con.TerminationMessagePath == "" {
						con.TerminationMessagePath = "/dev/termination-log"
					}
					if con.TerminationMessagePolicy == "" {
						con.TerminationMessagePolicy = corev1.TerminationMessageReadFile
					}
					if con.ImagePullPolicy == "" {
						con.ImagePullPolicy = corev1.PullIfNotPresent
					}

					// Add a default REST endpoint if none provided
					if pu.Endpoint == nil {
						pu.Endpoint = &machinelearningv1alpha2.Endpoint{Type: machinelearningv1alpha2.REST}
					}

					con.VolumeMounts = append(con.VolumeMounts, v1.VolumeMount{
						Name:      machinelearningv1alpha2.PODINFO_VOLUME_NAME,
						MountPath: machinelearningv1alpha2.PODINFO_VOLUME_PATH,
					})

					// Get existing Http or Grpc port and add Liveness and Readiness probes if they don't exist
					var portType string
					if pu.Endpoint.Type == machinelearningv1alpha2.REST {
						portType = "http"
					} else {
						portType = "grpc"
					}

					existingPort := getPort(portType, con.Ports)
					if existingPort == nil {
						con.Ports = append(con.Ports, v1.ContainerPort{Name: portType, ContainerPort: portNum, Protocol: corev1.ProtocolTCP})
					} else {
						portNum = existingPort.ContainerPort
					}
					if con.LivenessProbe == nil {
						con.LivenessProbe = &v1.Probe{Handler: v1.Handler{TCPSocket: &v1.TCPSocketAction{Port: intstr.FromString(portType)}}, InitialDelaySeconds: 60, PeriodSeconds: 5, SuccessThreshold: 1, FailureThreshold: 3, TimeoutSeconds: 1}
					}
					if con.ReadinessProbe == nil {
						con.ReadinessProbe = &v1.Probe{Handler: v1.Handler{TCPSocket: &v1.TCPSocketAction{Port: intstr.FromString(portType)}}, InitialDelaySeconds: 20, PeriodSeconds: 5, SuccessThreshold: 1, FailureThreshold: 3, TimeoutSeconds: 1}
					}

					// Add livecycle probe
					if con.Lifecycle == nil {
						con.Lifecycle = &v1.Lifecycle{PreStop: &v1.Handler{Exec: &v1.ExecAction{Command: []string{"/bin/sh", "-c", "/bin/sleep 10"}}}}
					}

					// Add Environment Variables
					con.Env = append(con.Env, []v1.EnvVar{
						v1.EnvVar{Name: machinelearningv1alpha2.ENV_PREDICTIVE_UNIT_SERVICE_PORT, Value: strconv.Itoa(int(portNum))},
						v1.EnvVar{Name: machinelearningv1alpha2.ENV_PREDICTIVE_UNIT_ID, Value: con.Name},
						v1.EnvVar{Name: machinelearningv1alpha2.ENV_PREDICTOR_ID, Value: p.Name},
						v1.EnvVar{Name: machinelearningv1alpha2.ENV_SELDON_DEPLOYMENT_ID, Value: mlDep.ObjectMeta.Name},
					}...)
					if len(pu.Parameters) > 0 {
						con.Env = append(con.Env, v1.EnvVar{Name: machinelearningv1alpha2.ENV_PREDICTIVE_UNIT_PARAMETERS, Value: getPredictiveUnitAsJson(pu.Parameters)})
					}

					// Set ports and hostname in predictive unit
					if _, hasSeparateEnginePod := mlDep.Spec.Annotations[machinelearningv1alpha2.ANNOTATION_SEPARATE_ENGINE]; j == 0 && !hasSeparateEnginePod {
						pu.Endpoint.ServiceHost = "localhost"
					} else {
						containerServiceValue := machinelearningv1alpha2.GetContainerServiceName(mlDep, p, con)
						pu.Endpoint.ServiceHost = containerServiceValue + "." + mlDep.ObjectMeta.Namespace + ".svc.cluster.local."
					}
					pu.Endpoint.ServicePort = portNum
				} else {
					// Add some defaults for easier diffs later in controller
					if con.TerminationMessagePath == "" {
						con.TerminationMessagePath = "/dev/termination-log"
					}
					if con.TerminationMessagePolicy == "" {
						con.TerminationMessagePolicy = corev1.TerminationMessageReadFile
					}
					if con.ImagePullPolicy == "" {
						con.ImagePullPolicy = corev1.PullIfNotPresent
					}
				}
			}

			// Add defaultMode to volumes ifnot set to ensure no changes when comparing later in controller
			for k := 0; k < len(cSpec.Spec.Volumes); k++ {
				vol := &cSpec.Spec.Volumes[k]
				if vol.Secret != nil && vol.Secret.DefaultMode == nil {
					var defaultMode = corev1.SecretVolumeSourceDefaultMode
					vol.Secret.DefaultMode = &defaultMode
				} else if vol.ConfigMap != nil && vol.ConfigMap.DefaultMode == nil {
					var defaultMode = corev1.ConfigMapVolumeSourceDefaultMode
					vol.ConfigMap.DefaultMode = &defaultMode
				} else if vol.DownwardAPI != nil && vol.DownwardAPI.DefaultMode == nil {
					var defaultMode = corev1.DownwardAPIVolumeSourceDefaultMode
					vol.DownwardAPI.DefaultMode = &defaultMode
				} else if vol.Projected != nil && vol.Projected.DefaultMode == nil {
					var defaultMode = corev1.ProjectedVolumeSourceDefaultMode
					vol.Projected.DefaultMode = &defaultMode
				}
			}
		}
	}

	return nil
}

var _ admission.Handler = &SeldonDeploymentCreateUpdateHandler{}

// Handle handles admission requests.
func (h *SeldonDeploymentCreateUpdateHandler) Handle(ctx context.Context, req types.Request) types.Response {
	obj := &machinelearningv1alpha2.SeldonDeployment{}

	err := h.Decoder.Decode(req, obj)
	if err != nil {
		return admission.ErrorResponse(http.StatusBadRequest, err)
	}
	copy := obj.DeepCopy()

	err = h.MutatingSeldonDeploymentFn(ctx, copy)

	if err != nil {
		return admission.ErrorResponse(http.StatusInternalServerError, err)
	}
	return admission.PatchResponse(obj, copy)
}

//var _ inject.Client = &SeldonDeploymentCreateUpdateHandler{}
//
//// InjectClient injects the client into the SeldonDeploymentCreateUpdateHandler
//func (h *SeldonDeploymentCreateUpdateHandler) InjectClient(c client.Client) error {
//	h.Client = c
//	return nil
//}

var _ inject.Decoder = &SeldonDeploymentCreateUpdateHandler{}

// InjectDecoder injects the decoder into the SeldonDeploymentCreateUpdateHandler
func (h *SeldonDeploymentCreateUpdateHandler) InjectDecoder(d types.Decoder) error {
	h.Decoder = d
	return nil
}

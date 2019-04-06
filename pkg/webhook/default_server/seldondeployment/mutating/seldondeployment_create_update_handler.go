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
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"net/http"
	"strconv"

	machinelearningv1alpha2 "github.com/seldonio/seldon-operator/pkg/apis/machinelearning/v1alpha2"
	controller "github.com/seldonio/seldon-operator/pkg/controller/seldondeployment"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/runtime/inject"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission/types"
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

func addPorts(pu *machinelearningv1alpha2.PredictiveUnit, portMap map[string]int32) {
	if _, present := portMap[pu.Name]; present {
		portNum := portMap[pu.Name]
		pu.Endpoint.ServicePort = portNum
		return
	} else {
		for i := 0; i < len(pu.Children); i++ {
			addPorts(&pu.Children[i], portMap)
		}
	}
}

func getPort(name string, ports []v1.ContainerPort) *v1.ContainerPort {
	for i := 0; i < len(ports); i++ {
		if ports[i].Name == name {
			return &ports[i]
		}
	}
	return nil
}

const (
	ENV_PREDICTIVE_UNIT_SERVICE_PORT = "PREDICTIVE_UNIT_SERVICE_PORT"
	ENV_PREDICTIVE_UNIT_PARAMETERS   = "PREDICTIVE_UNIT_PARAMETERS"
	ENV_PREDICTIVE_UNIT_ID           = "PREDICTIVE_UNIT_ID"
	ENV_PREDICTOR_ID                 = "PREDICTOR_ID"
	ENV_SELDON_DEPLOYMENT_ID         = "SELDON_DEPLOYMENT_ID"
)

func getPredictiveUnitAsJson(params []machinelearningv1alpha2.Parameter) string {
	str, err := json.Marshal(params)
	if err != nil {
		return ""
	} else {
		return string(str)
	}
}

func (h *SeldonDeploymentCreateUpdateHandler) mutatingSeldonDeploymentFn(ctx context.Context, mlDep *machinelearningv1alpha2.SeldonDeployment) error {
	// TODO(user): implement your admission logic
	var nextPortNum int32 = 9000
	portMap := map[string]int32{}
	if mlDep.ObjectMeta.Namespace == "" {
		mlDep.ObjectMeta.Namespace = "default"
	}
	for i := 0; i < len(mlDep.Spec.Predictors); i++ {
		p := mlDep.Spec.Predictors[i]
		for j := 0; j < len(p.ComponentSpecs); j++ {
			cSpec := mlDep.Spec.Predictors[i].ComponentSpecs[j]

			//Add downwardAPI
			cSpec.Spec.Volumes = append(cSpec.Spec.Volumes, v1.Volume{Name: controller.PODINFO_VOLUME_NAME, VolumeSource: corev1.VolumeSource{
				DownwardAPI: &corev1.DownwardAPIVolumeSource{Items: []corev1.DownwardAPIVolumeFile{
					{Path: "annotations", FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.annotations"}}}}}})

			// create services for each container
			for k := 0; k < len(cSpec.Spec.Containers); k++ {
				con := &cSpec.Spec.Containers[0]
				//containerServiceKey := controller.GetPredictorServiceNameKey(&con)
				//

				if _, present := portMap[con.Name]; !present {
					portMap[con.Name] = nextPortNum
					nextPortNum++
				}
				portNum := portMap[con.Name]

				pu := controller.GetPredcitiveUnit(p.Graph, con.Name)

				if pu != nil {

					// Add a default REST endpoint if none provided
					if pu.Endpoint == nil {
						pu.Endpoint = &machinelearningv1alpha2.Endpoint{Type:machinelearningv1alpha2.REST}
					}

					con.VolumeMounts = append(con.VolumeMounts, v1.VolumeMount{
						Name:      controller.PODINFO_VOLUME_NAME,
						MountPath: controller.PODINFO_VOLUME_PATH,
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
						con.Ports = append(con.Ports, v1.ContainerPort{Name: "http", ContainerPort: portNum})
					} else {
						portNum = existingPort.ContainerPort
					}
					if con.LivenessProbe == nil {
						con.LivenessProbe = &v1.Probe{Handler: v1.Handler{TCPSocket: &v1.TCPSocketAction{Port: intstr.FromString(portType)}}, InitialDelaySeconds: 60, PeriodSeconds: 5}
					}
					if con.ReadinessProbe == nil {
						con.ReadinessProbe = &v1.Probe{Handler: v1.Handler{TCPSocket: &v1.TCPSocketAction{Port: intstr.FromString(portType)}}, InitialDelaySeconds: 20, PeriodSeconds: 5}
					}

					// Add livecycle probe
					if con.Lifecycle == nil {
						con.Lifecycle = &v1.Lifecycle{PreStop: &v1.Handler{Exec: &v1.ExecAction{Command: []string{"/bin/sh", "-c", "/bin/sleep 5"}}}}
					}

					// Add Environment Variables
					con.Env = append(con.Env, []v1.EnvVar{
						v1.EnvVar{Name: ENV_PREDICTIVE_UNIT_SERVICE_PORT, Value: strconv.Itoa(int(portNum))},
						v1.EnvVar{Name: ENV_PREDICTIVE_UNIT_ID, Value: con.Name},
						v1.EnvVar{Name: ENV_PREDICTOR_ID, Value: p.Name},
						v1.EnvVar{Name: ENV_SELDON_DEPLOYMENT_ID, Value: mlDep.ObjectMeta.Name},
					}...)
					if len(pu.Parameters) > 0 {
						con.Env = append(con.Env, v1.EnvVar{Name: ENV_PREDICTIVE_UNIT_PARAMETERS, Value: getPredictiveUnitAsJson(pu.Parameters)})
					}

					// Set ports and hostname in predictive unit
					if _, hasSeparateEnginePod := mlDep.Spec.Annotations[controller.ANNOTATION_SEPARATE_ENGINE]; j == 0 && !hasSeparateEnginePod {
						pu.Endpoint.ServiceHost = "localhost"
					} else {
						containerServiceValue := controller.GetContainerServiceName(mlDep, p, con)
						pu.Endpoint.ServiceHost = containerServiceValue + "." + mlDep.ObjectMeta.Namespace + ".svc.cluster.local."
					}
					pu.Endpoint.ServicePort = portNum
				}
			}
		}
	}

	/*
		for i := 0; i < len(mlDep.Spec.Predictors); i++ {
			p := mlDep.Spec.Predictors[i]
			addPorts(p.Graph, portMap)
		}
	*/

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

	err = h.mutatingSeldonDeploymentFn(ctx, copy)
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

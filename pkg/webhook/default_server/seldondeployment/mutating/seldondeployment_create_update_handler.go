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
	"os"
	"strconv"

	machinelearningv1alpha2 "github.com/seldonio/seldon-operator/pkg/apis/machinelearning/v1alpha2"
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
		addDefaultsToGraph(p.Graph)
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

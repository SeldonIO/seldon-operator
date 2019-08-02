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
	"fmt"
	machinelearningv1alpha2 "github.com/seldonio/seldon-operator/pkg/apis/machinelearning/v1alpha2"
	"github.com/seldonio/seldon-operator/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	"net/http"
	"os"
	"sigs.k8s.io/controller-runtime/pkg/runtime/inject"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission/types"
	"strconv"
)

var (
	DefaultSKLearnServerImageNameRest = "seldonio/sklearnserver_rest:0.1"
	DefaultSKLearnServerImageNameGrpc = "seldonio/sklearnserver_grpc:0.1"
	DefaultXGBoostServerImageNameRest = "seldonio/xgboostserver_rest:0.1"
	DefaultXGBoostServerImageNameGrpc = "seldonio/xgboostserver_grpc:0.1"
	DefaultTFServerImageNameRest      = "seldonio/tfserving-proxy_rest:0.3"
	DefaultTFServerImageNameGrpc      = "seldonio/tfserving-proxy_grpc:0.3"
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

	var firstPuPortNum int32 = 9000
	if env_preditive_unit_service_port, ok := os.LookupEnv("PREDICTIVE_UNIT_SERVICE_PORT"); ok {
		portNum, err := strconv.Atoi(env_preditive_unit_service_port)
		if err != nil {
			return err
		} else {
			firstPuPortNum = int32(portNum)
		}
	}
	nextPortNum := firstPuPortNum

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

		fmt.Println("predictor is now")
		jstr, _ := json.Marshal(p)
		fmt.Println(string(jstr))

		mlDep.Spec.Predictors[i] = p

		for j := 0; j < len(p.ComponentSpecs); j++ {
			cSpec := mlDep.Spec.Predictors[i].ComponentSpecs[j]

			// add service details for each container - looping this way as if containers in same pod and its the engine pod both need to be localhost
			for k := 0; k < len(cSpec.Spec.Containers); k++ {
				con := &cSpec.Spec.Containers[k]

				if _, present := portMap[con.Name]; !present {
					portMap[con.Name] = nextPortNum
					nextPortNum++
				}
				portNum := portMap[con.Name]

				pu := machinelearningv1alpha2.GetPredcitiveUnit(p.Graph, con.Name)

				if pu != nil {

					if pu.Endpoint == nil {
						pu.Endpoint = &machinelearningv1alpha2.Endpoint{Type: machinelearningv1alpha2.REST}
					}
					var portType string
					if pu.Endpoint.Type == machinelearningv1alpha2.GRPC {
						portType = "grpc"
					} else {
						portType = "http"
					}

					if con != nil {
						existingPort := utils.GetPort(portType, con.Ports)
						if existingPort != nil {
							portNum = existingPort.ContainerPort
						}
					}

					// Set ports and hostname in predictive unit so engine can read it from SDep
					// if this is the first componentSpec then it's the one to put the engine in - note using outer loop counter here
					if _, hasSeparateEnginePod := mlDep.Spec.Annotations[machinelearningv1alpha2.ANNOTATION_SEPARATE_ENGINE]; j == 0 && !hasSeparateEnginePod {
						pu.Endpoint.ServiceHost = "localhost"
					} else {
						containerServiceValue := machinelearningv1alpha2.GetContainerServiceName(mlDep, p, con)
						pu.Endpoint.ServiceHost = containerServiceValue + "." + mlDep.ObjectMeta.Namespace + ".svc.cluster.local."
					}
					pu.Endpoint.ServicePort = portNum
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

		pus := machinelearningv1alpha2.GetPredictiveUnitList(p.Graph)

		//some pus might not have a container spec so pick those up
		for l := 0; l < len(pus); l++ {
			pu := pus[l]

			con := utils.GetContainerForPredictiveUnit(&p, pu.Name)

			//only assign host and port if there's a container or it's a prepackaged model server
			if *pu.Implementation != machinelearningv1alpha2.SKLEARN_SERVER && *pu.Implementation != machinelearningv1alpha2.XGBOOST_SERVER && *pu.Implementation != machinelearningv1alpha2.TENSORFLOW_SERVER && (con == nil || con.Name == "") {
				continue
			}

			if _, present := portMap[pu.Name]; !present {
				portMap[pu.Name] = nextPortNum
				nextPortNum++
			}
			portNum := portMap[pu.Name]
			// Add a default REST endpoint if none provided
			// pu needs to have an endpoint as engine reads it from SDep in order to direct graph traffic
			// probes etc will be added later by controller
			if pu.Endpoint == nil {
				pu.Endpoint = &machinelearningv1alpha2.Endpoint{Type: machinelearningv1alpha2.REST}
			}
			var portType string
			if pu.Endpoint.Type == machinelearningv1alpha2.GRPC {
				portType = "grpc"
			} else {
				portType = "http"
			}

			if con != nil {
				existingPort := utils.GetPort(portType, con.Ports)
				if existingPort != nil {
					portNum = existingPort.ContainerPort
				}
			}
			// Set ports and hostname in predictive unit so engine can read it from SDep
			// if this is the firstPuPortNum then we've not added engine yet so put the engine in here
			if pu.Endpoint.ServiceHost == "" {
				if _, hasSeparateEnginePod := mlDep.Spec.Annotations[machinelearningv1alpha2.ANNOTATION_SEPARATE_ENGINE]; portNum == firstPuPortNum && !hasSeparateEnginePod {
					pu.Endpoint.ServiceHost = "localhost"
				} else {
					containerServiceValue := machinelearningv1alpha2.GetContainerServiceName(mlDep, p, con)
					pu.Endpoint.ServiceHost = containerServiceValue + "." + mlDep.ObjectMeta.Namespace + ".svc.cluster.local."
				}
			}
			if pu.Endpoint.ServicePort == 0 {
				pu.Endpoint.ServicePort = portNum
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

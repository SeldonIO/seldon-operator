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

package validating

import (
	"context"
	"github.com/seldonio/seldon-operator/pkg/utils"
	"net/http"

	machinelearningv1alpha2 "github.com/seldonio/seldon-operator/pkg/apis/machinelearning/v1alpha2"
	"sigs.k8s.io/controller-runtime/pkg/runtime/inject"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission/types"
)

func init() {
	webhookName := "validating-create-update-seldondeployment"
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

// Check the predictive units to ensure the graph matches up with defined containers.
func checkPredictiveUnits(pu *machinelearningv1alpha2.PredictiveUnit, p *machinelearningv1alpha2.PredictorSpec) (bool, string) {
	if *pu.Implementation == machinelearningv1alpha2.UNKNOWN_IMPLEMENTATION {

		if utils.GetContainerForPredictiveUnit(p, pu.Name) == nil {
			return false, "Can't find container for Preditive Unit " + pu.Name
		}

		if *pu.Type == machinelearningv1alpha2.UNKNOWN_TYPE && (pu.Methods == nil || len(*pu.Methods) == 0) {
			return false, "Predictive Unit " + pu.Name + " has no implementation methods defined. Change to a know type or add what methods it defines"
		}

	} else if *pu.Implementation == machinelearningv1alpha2.SKLEARN_SERVER || *pu.Implementation == machinelearningv1alpha2.XGBOOST_SERVER {
		if pu.ModelURI == "" {
			return false, "Predictive unit " + pu.Name + " modelUri required when using standalone servers"
		}
	}

	for i := 0; i < len(pu.Children); i++ {
		checkPredictiveUnits(&pu.Children[i], p)
	}

	return true, ""
}

func checkTraffic(mlDep *machinelearningv1alpha2.SeldonDeployment) (bool, string) {
	var trafficSum int32 = 0
	for i := 0; i < len(mlDep.Spec.Predictors); i++ {
		p := mlDep.Spec.Predictors[i]
		trafficSum = trafficSum + p.Traffic
	}
	if trafficSum != 100 && len(mlDep.Spec.Predictors) > 1 {
		return false, "Traffic must sum to 100 for multiple predictors"
	}
	if trafficSum > 0 && trafficSum < 100 && len(mlDep.Spec.Predictors) == 1 {
		return false, "Traffic must sum be 100 for a single predictor when set"
	}

	return true, ""
}

func (h *SeldonDeploymentCreateUpdateHandler) validatingSeldonDeploymentFn(ctx context.Context, obj *machinelearningv1alpha2.SeldonDeployment) (bool, string, error) {

	predictorNames := make(map[string]bool)
	for i := 0; i < len(obj.Spec.Predictors); i++ {
		p := obj.Spec.Predictors[i]
		if _, present := predictorNames[p.Name]; present {
			return false, "Duplicate Predictor Name " + p.Name, nil
		}
		predictorNames[p.Name] = true
		ok, reason := checkPredictiveUnits(p.Graph, &p)
		if !ok {
			return false, reason, nil
		}
	}
	ok, reason := checkTraffic(obj)
	if !ok {
		return false, reason, nil
	}
	return true, "allowed to be admitted", nil
}

var _ admission.Handler = &SeldonDeploymentCreateUpdateHandler{}

// Handle handles admission requests.
func (h *SeldonDeploymentCreateUpdateHandler) Handle(ctx context.Context, req types.Request) types.Response {
	obj := &machinelearningv1alpha2.SeldonDeployment{}

	err := h.Decoder.Decode(req, obj)
	if err != nil {
		return admission.ErrorResponse(http.StatusBadRequest, err)
	}

	allowed, reason, err := h.validatingSeldonDeploymentFn(ctx, obj)
	if err != nil {
		return admission.ErrorResponse(http.StatusInternalServerError, err)
	}
	return admission.ValidationResponse(allowed, reason)
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

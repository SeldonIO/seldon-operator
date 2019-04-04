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

func (h *SeldonDeploymentCreateUpdateHandler) validatingSeldonDeploymentFn(ctx context.Context, obj *machinelearningv1alpha2.SeldonDeployment) (bool, string, error) {
	// TODO(user): implement your admission logic
	if obj.Spec.Name != "foobar" {
		return false, "name must be foobar", nil
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

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

package v1alpha2

import (
	autoscalingv2beta2 "k8s.io/api/autoscaling/v2beta1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// SeldonDeploymentSpec defines the desired state of SeldonDeployment
type SeldonDeploymentSpec struct {
	Name        string            `json:"name,omitempty" protobuf:"string,1,opt,name=name"`
	Predictors  []PredictorSpec   `json:"predictors,omitempty" protobuf:"bytes,2,opt,name=name"`
	OauthKey    string            `json:"oauth_key,omitempty" protobuf:"string,3,opt,name=oauth_key"`
	OauthSecret string            `json:"oauth_secret,omitempty" protobuf:"string,4,opt,name=oauth_secret"`
	Annotations map[string]string `json:"annotations,omitempty" protobuf:"bytes,5,opt,name=annotations"`
}

type PredictorSpec struct {
	Name            string                  `json:"name,omitempty" protobuf:"string,1,opt,name=name"`
	Graph           *PredictiveUnit         `json:"graph,omitempty" protobuf:"bytes,2,opt,name=predictiveUnit"`
	ComponentSpecs  []*SeldonPodSpec        `json:"componentSpecs,omitempty" protobuf:"bytes,3,opt,name=componentSpecs"`
	Replicas        int32                   `json:"replicas,omitempty" protobuf:"string,4,opt,name=replicas"`
	Annotations     map[string]string       `json:"annotations,omitempty" protobuf:"bytes,5,opt,name=annotations"`
	EngineResources v1.ResourceRequirements `json:"engineResources,omitempty" protobuf:"bytes,6,opt,name=engineResources"`
	Labels          map[string]string       `json:"labels,omitempty" protobuf:"bytes,7,opt,name=labels"`
	SvcOrchSpec     SvcOrchSpec             `json:"svcOrchSpec,omitempty" protobuf:"bytes,8,opt,name=svcOrchSpec"`
	Traffic         int32                   `json:"traffic,omitempty" protobuf:"bytes,9,opt,name=traffic"`
}

type SvcOrchSpec struct {
	Resources *v1.ResourceRequirements `json:"resources,omitempty" protobuf:"bytes,1,opt,name=resources"`
	Env       *v1.EnvVar               `json:"env,omitempty" protobuf:"bytes,2,opt,name=env"`
}

type SeldonPodSpec struct {
	Metadata metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
	Spec     v1.PodSpec        `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`
	HpaSpec  *SeldonHpaSpec    `json:"hpaSpec,omitempty" protobuf:"bytes,3,opt,name=hpaSpec"`
}

type SeldonHpaSpec struct {
	MinReplicas int32                         `json:"minReplicas,omitempty" protobuf:"int,1,opt,name=minReplicas"`
	MaxReplicas int32                         `json:"maxReplicas,omitempty" protobuf:"int,2,opt,name=maxReplicas"`
	Metrics     autoscalingv2beta2.MetricSpec `json:"metrics,omitempty" protobuf:"bytes,3,opt,name=metrics"`
}

type PredictiveUnitType string

const (
	UNKNOWN_TYPE       PredictiveUnitType = "UNKOWN_TYPE"
	ROUTER             PredictiveUnitType = "ROUTER"
	COMBINER           PredictiveUnitType = "COMBINER"
	MODEL              PredictiveUnitType = "MODEL"
	TRANSFORMER        PredictiveUnitType = "TRANSFORMER"
	OUTPUT_TRANSFORMER PredictiveUnitType = "OUTPUT_TRANSFORMER"
)

type PredictiveUnitImplementation string

const (
	UNKNOWN_IMPLEMENTATION PredictiveUnitImplementation = "UNKNOWN_IMPLEMENTATION"
	SIMPLE_MODEL           PredictiveUnitImplementation = "SIMPLE_MODEL"
	SIMPLE_ROUTER          PredictiveUnitImplementation = "SIMPLE_ROUTER"
	RANDOM_ABTEST          PredictiveUnitImplementation = "RANDOM_ABTEST"
	AVERAGE_COMBINER       PredictiveUnitImplementation = "AVERAGE_COMBINER"
)

type PredictiveUnitMethod string

const (
	TRANSFORM_INPUT  PredictiveUnitMethod = "TRANSFORM_INPUT"
	TRANSFORM_OUTPUT PredictiveUnitMethod = "TRANSFORM_OUTPUT"
	ROUTE            PredictiveUnitMethod = "ROUTE"
	AGGREGATE        PredictiveUnitMethod = "AGGREGATE"
	SEND_FEEDBACK    PredictiveUnitMethod = "SEND_FEEDBACK"
)

type EndpointType string

const (
	REST EndpointType = "REST"
	GRPC EndpointType = "GRPC"
)

type Endpoint struct {
	ServiceHost string       `json:"service_host,omitempty" protobuf:"string,1,opt,name=service_host"`
	ServicePort int32        `json:"service_port,omitempty" protobuf:"int32,2,opt,name=service_port"`
	Type        EndpointType `json:"type,omitempty" protobuf:"int,3,opt,name=type"`
}

type ParmeterType string

const (
	INT    ParmeterType = "INT"
	FLOAT  ParmeterType = "FLOAT"
	DOUBLE ParmeterType = "DOUBLE"
	STRING ParmeterType = "STRING"
	BOOL   ParmeterType = "BOOL"
)

type Parameter struct {
	Name  string       `json:"name,omitempty" protobuf:"string,1,opt,name=name"`
	Value string       `json:"value,omitempty" protobuf:"string,2,opt,name=value"`
	Type  ParmeterType `json:"type,omitempty" protobuf:"int,3,opt,name=type"`
}

type PredictiveUnit struct {
	Name           string                       `json:"name,omitempty" protobuf:"string,1,opt,name=name"`
	Children       []PredictiveUnit             `json:"children,omitempty" protobuf:"bytes,2,opt,name=children"`
	Type           PredictiveUnitType           `json:"type,omitempty" protobuf:"int,3,opt,name=type"`
	Implementation PredictiveUnitImplementation `json:"implementation,omitempty" protobuf:"int,4,opt,name=implementation"`
	Methods        PredictiveUnitMethod         `json:"methods,omitempty" protobuf:"int,5,opt,name=methods"`
	Endpoint       *Endpoint                    `json:"endpoint,omitempty" protobuf:"bytes,6,opt,name=endpoint"`
	Parameters     []Parameter                  `json:"parameters,omitempty" protobuf:"bytes,7,opt,name=parameters"`
}

type PredictorStatus struct {
	Name              string `json:"name,omitempty" protobuf:"string,1,opt,name=name"`
	Status            string `json:"status,omitempty" protobuf:"string,2,opt,name=status"`
	Description       string `json:"description,omitempty" protobuf:"string,3,opt,name=description"`
	Replicas          int32  `json:"replicas,omitempty" protobuf:"string,4,opt,name=replicas"`
	ReplicasAvailable int32  `json:"replicasAvailable,omitempty" protobuf:"string,5,opt,name=replicasAvailable"`
}

// SeldonDeploymentStatus defines the observed state of SeldonDeployment
type SeldonDeploymentStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	State           string          `json:"state,omitempty" protobuf:"string,1,opt,name=state"`
	Description     string          `json:"description,omitempty" protobuf:"string,2,opt,name=description"`
	PredictorStatus PredictorStatus `json:"predictorStatus,omitempty" protobuf:"bytes,3,opt,name=predictorStatus"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// SeldonDeployment is the Schema for the seldondeployments API
// +k8s:openapi-gen=true
// +kubebuilder:resource:shortName=sdep
// +kubebuilder:subresource:status
type SeldonDeployment struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SeldonDeploymentSpec   `json:"spec,omitempty"`
	Status SeldonDeploymentStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// SeldonDeploymentList contains a list of SeldonDeployment
type SeldonDeploymentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SeldonDeployment `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SeldonDeployment{}, &SeldonDeploymentList{})
}

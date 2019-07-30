package utils

import (
	"encoding/json"
	machinelearningv1alpha2 "github.com/seldonio/seldon-operator/pkg/apis/machinelearning/v1alpha2"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"strings"
)

func GetContainerForPredictiveUnit(p *machinelearningv1alpha2.PredictorSpec, name string) *v1.Container {
	for j := 0; j < len(p.ComponentSpecs); j++ {
		cSpec := p.ComponentSpecs[j]
		for k := 0; k < len(cSpec.Spec.Containers); k++ {
			c := &cSpec.Spec.Containers[k]
			if c.Name == name {
				return c
			}
		}
	}
	return nil
}

func GetPredictiveUnitAsJson(params []machinelearningv1alpha2.Parameter) string {
	str, err := json.Marshal(params)
	if err != nil {
		return ""
	} else {
		return string(str)
	}
}

func GetSeldonPodSpecForPredictiveUnit(p *machinelearningv1alpha2.PredictorSpec, name string) *machinelearningv1alpha2.SeldonPodSpec {
	for j := 0; j < len(p.ComponentSpecs); j++ {
		cSpec := p.ComponentSpecs[j]
		for k := 0; k < len(cSpec.Spec.Containers); k++ {
			c := &cSpec.Spec.Containers[k]
			//the podSpec will have a container matching the PU name
			if c.Name == name {
				return cSpec
			}
		}
	}
	return nil
}

func GetContainerForDeployment(deploy *appsv1.Deployment, name string) *v1.Container {
	var userContainer *v1.Container
	for idx, container := range deploy.Spec.Template.Spec.Containers {
		if strings.Compare(container.Name, name) == 0 {
			userContainer = &deploy.Spec.Template.Spec.Containers[idx]
			return userContainer
		}
	}
	return nil
}

func HasEnvVar(envVars []v1.EnvVar, name string) bool {
	for _, envVar := range envVars {
		if envVar.Name == name {
			return true
		}
	}
	return false
}

package utils

import (
	machinelearningv1alpha2 "github.com/seldonio/seldon-operator/pkg/apis/machinelearning/v1alpha2"
	v1 "k8s.io/api/core/v1"
)

func GetContainerForPredictiveUnit(p *machinelearningv1alpha2.PredictorSpec, name string) *v1.Container {
	for j := 0; j < len(p.ComponentSpecs); j++ {
		cSpec := p.ComponentSpecs[j]
		for k := 0; k < len(cSpec.Spec.Containers); k++ {
			c := cSpec.Spec.Containers[k]
			if c.Name == name {
				return &c
			}
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

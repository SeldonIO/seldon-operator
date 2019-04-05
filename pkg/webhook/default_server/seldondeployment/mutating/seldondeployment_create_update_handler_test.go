package mutating


import (
	"fmt"
	machinelearningv1alpha2 "github.com/seldonio/seldon-operator/pkg/apis/machinelearning/v1alpha2"
	"testing"
)

func TestParameterJson(t *testing.T) {
	params := []machinelearningv1alpha2.Parameter{machinelearningv1alpha2.Parameter{Name:"foo",Type:"FLOAT",Value:"1"}}
	jstr := getPredictiveUnitAsJson(params)
	fmt.Println(jstr)
}
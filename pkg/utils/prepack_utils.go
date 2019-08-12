package utils

import (
	machinelearningv1alpha2 "github.com/seldonio/seldon-operator/pkg/apis/machinelearning/v1alpha2"
	"github.com/seldonio/seldon-operator/pkg/constants"
	"k8s.io/api/core/v1"
)

func SetImageNameForPrepackContainer(pu *machinelearningv1alpha2.PredictiveUnit, c *v1.Container) {
	//Add missing fields
	// Add image
	if c.Image == "" {
		if *pu.Implementation == machinelearningv1alpha2.SKLEARN_SERVER {

			if pu.Endpoint.Type == machinelearningv1alpha2.REST {
				c.Image = constants.DefaultSKLearnServerImageNameRest
			} else {
				c.Image = constants.DefaultSKLearnServerImageNameGrpc
			}

		} else if *pu.Implementation == machinelearningv1alpha2.XGBOOST_SERVER {

			if pu.Endpoint.Type == machinelearningv1alpha2.REST {
				c.Image = constants.DefaultXGBoostServerImageNameRest
			} else {
				c.Image = constants.DefaultXGBoostServerImageNameGrpc
			}

		} else if *pu.Implementation == machinelearningv1alpha2.TENSORFLOW_SERVER {

			if pu.Endpoint.Type == machinelearningv1alpha2.REST {
				c.Image = constants.DefaultTFServerImageNameRest
			} else {
				c.Image = constants.DefaultTFServerImageNameGrpc
			}

		} else if *pu.Implementation == machinelearningv1alpha2.MLFLOW_SERVER {

			if pu.Endpoint.Type == machinelearningv1alpha2.REST {
				c.Image = constants.DefaultMLFlowServerImageNameRest
			} else {
				c.Image = constants.DefaultMLFlowServerImageNameGrpc
			}

		}
	}
}

func IsPrepack(pu *machinelearningv1alpha2.PredictiveUnit) bool {
	return *pu.Implementation == machinelearningv1alpha2.SKLEARN_SERVER || *pu.Implementation == machinelearningv1alpha2.XGBOOST_SERVER || *pu.Implementation == machinelearningv1alpha2.TENSORFLOW_SERVER || *pu.Implementation == machinelearningv1alpha2.MLFLOW_SERVER
}

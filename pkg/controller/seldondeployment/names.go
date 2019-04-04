package seldondeployment

import (
	"crypto/md5"
	"encoding/hex"
	machinelearningv1alpha2 "github.com/seldonio/seldon-operator/pkg/apis/machinelearning/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	"regexp"
	"strings"
)

func hash(text string) string {
	hasher := md5.New()
	hasher.Write([]byte(text))
	return hex.EncodeToString(hasher.Sum(nil))
}

func containerHash(podSpec machinelearningv1alpha2.SeldonPodSpec) string {
	s := []string{}
	for i := 0; i < len(podSpec.Spec.Containers); i++ {
		c := podSpec.Spec.Containers[i]
		s = append(s, c.Name)
		s = append(s, c.Image)
	}
	return hash(strings.Join(s, ":"))
}

func GetSeldonDeploymentName(mlDep *machinelearningv1alpha2.SeldonDeployment) string {
	return mlDep.Spec.Name + "-" + mlDep.ObjectMeta.Name
}

func GetDeploymentName(mlDep *machinelearningv1alpha2.SeldonDeployment, predictorSpec machinelearningv1alpha2.PredictorSpec, podSpec machinelearningv1alpha2.SeldonPodSpec) string {
	if len(podSpec.Metadata.Name) != 0 {
		return podSpec.Metadata.Name
	} else {
		name := mlDep.Spec.Name + "-" + predictorSpec.Name + "-" + containerHash(podSpec)
		if len(name) > 63 {
			return "seldon-" + hash(name)
		} else {
			return name
		}
	}
}

func cleanContainerName(name string) string {
	var re = regexp.MustCompile("[^-a-z0-9]")
	return re.ReplaceAllString(strings.ToLower(name), "-")
}

func GetContainerServiceName(mlDep *machinelearningv1alpha2.SeldonDeployment, predictorSpec machinelearningv1alpha2.PredictorSpec, c *corev1.Container) string {
	containerImageName := cleanContainerName(c.Name)
	svcName := mlDep.Spec.Name + "-" + predictorSpec.Name + "-" + c.Name + "-" + containerImageName
	if len(svcName) > 63 {
		svcName = "seldon" + "-" + containerImageName + "-" + hash(svcName)
		if len(svcName) > 63 {
			return "seldon-" + hash(svcName)
		} else {
			return svcName
		}
	}
}

func GetPredictorServiceNameKey(c *corev1.Container) string {
	return Label_seldon_app + "-" + c.Name
}

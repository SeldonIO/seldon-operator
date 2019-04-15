package seldondeployment

import (
	"fmt"
	machinelearningv1alpha2 "github.com/seldonio/seldon-operator/pkg/apis/machinelearning/v1alpha2"
	"gopkg.in/yaml.v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"strings"
	"testing"
)

func TestAmbassadorBasic(t *testing.T) {
	mlDep := machinelearningv1alpha2.SeldonDeployment{ObjectMeta: metav1.ObjectMeta{Name: "mymodel"}}
	s, err := getAmbassadorConfigs(&mlDep, "myservice", 9000, 5000)
	if err != nil {
		t.Fatalf("Config format error")
	}
	fmt.Printf("%s\n\n", s)
	parts := strings.Split(s, "---\n")[1:]

	if len(parts) != 4 {
		t.Fatalf("Bad number of configs returned %d", len(parts))
	}

	for _, part := range parts {
		c := AmbassadorConfig{}
		fmt.Printf("Config: %s", part)

		err = yaml.Unmarshal([]byte(s), &c)
		if err != nil {
			t.Fatalf("Failed to unmarshall")
		}

		if len(c.Headers) > 0 {
			t.Fatalf("Found headers")
		}
		if c.Prefix != "/seldon/mymodel/" {
			t.Fatalf("Found bad prefix %s", c.Prefix)
		}
	}

}

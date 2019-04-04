package seldondeployment

import (
	"testing"
)

func TestCleanImageName(t *testing.T) {
	//g := gomega.NewGomegaWithT(t)
	name2 := cleanContainerName("AB_C")
	if name2 != "ab-c" {
		t.Fatalf("should be abc: %s",name2)
	}
}

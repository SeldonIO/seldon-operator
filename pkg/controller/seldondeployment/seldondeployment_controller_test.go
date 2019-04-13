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

package seldondeployment

import (
	"context"
	"encoding/json"
	"github.com/onsi/gomega"
	machinelearningv1alpha2 "github.com/seldonio/seldon-operator/pkg/apis/machinelearning/v1alpha2"
	"github.com/seldonio/seldon-operator/pkg/webhook/default_server/seldondeployment/mutating"
	"io/ioutil"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/api/autoscaling/v2beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"path/filepath"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"testing"
	"time"
)

var c client.Client

const timeout = time.Second * 500

func helperLoadBytes(t *testing.T, name string) []byte {
	path := filepath.Join("testdata", name) // relative path
	bytes, err := ioutil.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return bytes
}

func TestReconcileSimpleModel(t *testing.T) {
	var expectedRequest = reconcile.Request{NamespacedName: types.NamespacedName{Name: "seldon-model", Namespace: "default"}}

	g := gomega.NewGomegaWithT(t)

	instance := &machinelearningv1alpha2.SeldonDeployment{}

	bStr := helperLoadBytes(t, "model.json")
	json.Unmarshal(bStr, instance)

	updateHandler := &mutating.SeldonDeploymentCreateUpdateHandler{}
	updateHandler.MutatingSeldonDeploymentFn(context.TODO(), instance)

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, err := manager.New(cfg, manager.Options{})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	c = mgr.GetClient()

	recFn, requests := SetupTestReconcile(newReconciler(mgr))
	g.Expect(add(mgr, recFn)).NotTo(gomega.HaveOccurred())

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	// Create the SeldonDeployment object and expect the Reconcile and Deployment to be created
	err = c.Create(context.TODO(), instance)
	// The instance object may not be a valid object because it might be missing some required fields.
	// Please modify the instance object by adding required fields and then remove the following if statement.
	if apierrors.IsInvalid(err) {
		t.Logf("failed to create object, got an invalid object error: %v", err)
		return
	}
	g.Expect(err).NotTo(gomega.HaveOccurred())
	// delete the SeldonDeployment at end of test
	defer c.Delete(context.TODO(), instance)
	g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))

	//var depKey = types.NamespacedName{Name: "test-deployment-example-8082e22", Namespace: "default"}
	depName := machinelearningv1alpha2.GetDeploymentName(instance, instance.Spec.Predictors[0], instance.Spec.Predictors[0].ComponentSpecs[0])
	var depKey = types.NamespacedName{Name: depName, Namespace: "default"}
	deploy := &appsv1.Deployment{}
	// We should eventually get the created deployment
	g.Eventually(func() error { return c.Get(context.TODO(), depKey, deploy) }, timeout).
		Should(gomega.Succeed())

	// Delete the Deployment and expect Reconcile to be called for Deployment deletion
	g.Expect(c.Delete(context.TODO(), deploy)).NotTo(gomega.HaveOccurred())
	// wait until we have seen a new reconcile processed
	g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))
	// Keep checking until we find the deployment again
	g.Eventually(func() error { return c.Get(context.TODO(), depKey, deploy) }, timeout).
		Should(gomega.Succeed())

	// Manually delete Deployment since GC isn't enabled in the test control plane
	g.Eventually(func() error { return c.Delete(context.TODO(), deploy) }, timeout).
		Should(gomega.MatchError("deployments.apps \"" + depName + "\" not found"))

}

func TestReconcileHpaModel(t *testing.T) {
	var expectedRequest = reconcile.Request{NamespacedName: types.NamespacedName{Name: "seldon-model", Namespace: "default"}}

	g := gomega.NewGomegaWithT(t)

	instance := &machinelearningv1alpha2.SeldonDeployment{}

	bStr := helperLoadBytes(t, "model_with_hpa.json")
	json.Unmarshal(bStr, instance)

	updateHandler := &mutating.SeldonDeploymentCreateUpdateHandler{}
	updateHandler.MutatingSeldonDeploymentFn(context.TODO(), instance)

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, err := manager.New(cfg, manager.Options{})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	c = mgr.GetClient()

	recFn, requests := SetupTestReconcile(newReconciler(mgr))
	g.Expect(add(mgr, recFn)).NotTo(gomega.HaveOccurred())

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	// Create the SeldonDeployment object and expect the Reconcile and Deployment to be created
	err = c.Create(context.TODO(), instance)
	// The instance object may not be a valid object because it might be missing some required fields.
	// Please modify the instance object by adding required fields and then remove the following if statement.
	if apierrors.IsInvalid(err) {
		t.Logf("failed to create object, got an invalid object error: %v", err)
		return
	}
	g.Expect(err).NotTo(gomega.HaveOccurred())
	// delete the SeldonDeployment at end of test
	defer c.Delete(context.TODO(), instance)
	g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))

	//var depKey = types.NamespacedName{Name: "test-deployment-example-8082e22", Namespace: "default"}
	depName := machinelearningv1alpha2.GetDeploymentName(instance, instance.Spec.Predictors[0], instance.Spec.Predictors[0].ComponentSpecs[0])
	var depKey = types.NamespacedName{Name: depName, Namespace: "default"}
	deploy := &appsv1.Deployment{}
	// We should eventually get the created deployment
	g.Eventually(func() error { return c.Get(context.TODO(), depKey, deploy) }, timeout).
		Should(gomega.Succeed())

	hpa := &v2beta1.HorizontalPodAutoscaler{}
	g.Eventually(func() error { return c.Get(context.TODO(), depKey, hpa) }, timeout).
		Should(gomega.Succeed())

	// Delete the Deployment and expect Reconcile to be called for Deployment deletion
	g.Expect(c.Delete(context.TODO(), deploy)).NotTo(gomega.HaveOccurred())
	// wait until we have seen a new reconcile processed
	g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))
	// Keep checking until we find the deployment again
	g.Eventually(func() error { return c.Get(context.TODO(), depKey, deploy) }, timeout).
		Should(gomega.Succeed())

	// Manually delete Deployment since GC isn't enabled in the test control plane
	g.Eventually(func() error { return c.Delete(context.TODO(), deploy) }, timeout).
		Should(gomega.MatchError("deployments.apps \"" + depName + "\" not found"))

}

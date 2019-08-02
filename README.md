# Seldon Core Go Controller
A Go controller for the Seldon Core CRD

## Developer Notes

Clone into GOPATH.

Kubebuilder is required to build the project.
Run `export GO111MODULE=off`
Run with `make`

Key files:

* mutating/seldondeployment_create_update_handler in the webhook mutates the SeldonDeployment to add info needed by the engine to direct traffic in the graph.
* seldondeployment_controller has the main loop code for the controller and its package has supporting functions. Parses graph and creates list of components, which are then submitted to k8s. Predictor service is created and SeldonDeployment status updated when everything else is ready.

### Historical Notes
This project was created with kubebuilder using the following commands.

```
kubebuilder init --domain seldon.io --license apache2 --owner "The Seldon Authors"
kubebuilder create api --group machinelearning --version v1alpha2 --kind SeldonDeployment
kubebuilder alpha webhook --group machinelearning --version v1alpha2 --kind SeldonDeployment --type=validating --operations=create,update
kubebuilder alpha webhook --group machinelearning --version v1alpha2 --kind SeldonDeployment --type=mutating --operations=create,update
```

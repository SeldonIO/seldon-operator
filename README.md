# Seldon Core Go Controller
A Go controller for the Seldon Core CRD


### Historical Notes
This project was created with kubebuilder using the following commands.

```
kubebuilder init --domain seldon.io --license apache2 --owner "The Seldon Authors"
kubebuilder create api --group machinelearning --version v1alpha2 --kind SeldonDeployment
kubebuilder alpha webhook --group machinelearning --version v1alpha2 --kind SeldonDeployment --type=validating --operations=create,update
kubebuilder alpha webhook --group machinelearning --version v1alpha2 --kind SeldonDeployment --type=mutating --operations=create,update
```

# Local Admission WebHooks in Minikube

Follows steps [here](https://medium.com/tarkalabs/proxying-services-into-minikube-8355db0065fd)

```
minikube ssh
```

edit the /etc/ssh/sshd_config file and set the GatewayPorts directive to yes.

restart sshd

```
sudo systemctl reload sshd
```

Port forward to your local GoLand server running, from minikube:


```
ssh -i $(minikube ssh-key) docker@$(minikube ip) -R 9876:localhost:9876
```

When running in GoLand locally, ensure you have a "/tmp/cert" folder and:

  * comment out the Secrets in `webhook/default_server/server.go`. This will make the WebHook server write the cert locally to filesystem.
  * comment out the Service as you will create it.


When server started create service and endpoint following steps described [here](https://cloud.google.com/blog/products/gcp/kubernetes-best-practices-mapping-external-services)

```
kubectl create -f webhook-svc.yaml
kubectl create -f endpoint-svc.yaml
```

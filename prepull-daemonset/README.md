# Description

The daemonset can be used to pre-pull large images to [Windows node pools on GKE](https://cloud.google.com/kubernetes-engine/docs/how-to/creating-a-cluster-windows). The daemonset supports both Docker and Containerd node pools.

# Configuring the daemonset

A list of desired images that is required to be pre-pulled on every Windows node can be specified in [daemonset.yaml](daemonset.yaml):
```
          pull gcr.io/my-registry/image-1:tag1 &&
          pull gcr.io/my-registry/image-2@sha256:some-digest
```

The daemonset initContainer uses a [nanoserver image](https://hub.docker.com/_/microsoft-windows-nanoserver) to run. :1809 tag supoorts Windows Server LTSC (2019). Please adjust to the proper LTSC or SAC tag as needed:
```
      - name: pre-pull-large-images
        image: mcr.microsoft.com/windows/nanoserver:1809
```

# Installation through CLI

```
$ kubectl create -f daemonset.yaml
```

# Checking logs

The logs from the daemonset pods shows the output from [crictl](https://github.com/kubernetes-sigs/cri-tools/blob/master/docs/crictl.md) tool used to pre-pull, as an example:
```
$ kubectl logs pre-pull-rl2d7 -c pre-pull-large-images

C:\>tools\crictl --debug pull <some-image-path>   
time="2022-03-25T18:07:58Z" level=debug msg="get image connection"
time="2022-03-25T18:07:58Z" level=warning msg="image connect using default endpoints: [npipe:////./pipe/dockershim npipe:////./pipe/containerd npipe:////./pipe/crio]. As the default settings are now deprecated, you should set the endpoint instead."
time="2022-03-25T18:08:00Z" level=debug msg="connect using endpoint 'npipe:////./pipe/containerd' with '2s' timeout"
time="2022-03-25T18:08:00Z" level=debug msg="connected successfully using endpoint: npipe:////./pipe/containerd"
time="2022-03-25T18:08:00Z" level=debug msg="PullImageRequest: &PullImageRequest{Image:&ImageSpec{Image:<some-image-path>,Annotations:map[string]string{},},Auth:nil,SandboxConfig:nil,}"
time="2022-03-25T18:08:00Z" level=debug msg="PullImageResponse: &PullImageResponse{ImageRef:sha256:<some-digest>,}"
Image is up to date for sha256:<some-digest>
```
# Description

This tool is used to create a GKE Windows builder to build multi-arch
containers using Cloud Build.

Customized from
[cloud-builders-community/windows-builder](https://github.com/GoogleCloudPlatform/cloud-builders-community/tree/master/windows-builder).

# Maintaining this builder

Update the `versionMap` in [main.go](builder/main.go) when a new Windows SAC or
LTSC version comes out.

# Building the gke-windows-builder container

## Louhi

We run several
[flows](http://louhi.dev/?expandedFlows=f4e0769a-5068-4a39-b17a-f270ba368c83%2C55a7c4c9-07be-4f00-a707-c84092b5106c%2Ca6702de6-8e13-4a5d-87fe-d39719eeedff%2Cd5ffe6ec-cecc-4a93-95b0-9a0e630321e9&projectId=5116198637797376#/flows)
in the GKE Windows Louhi project that build and test the gke-windows-builder.
The code for those flows is
[here](http://louhi-config-internal.git.corp.google.com/gke-windows/+/refs/heads/master).
The flows are:

*   *gke-windows-builder_ci*: continuous build.
*   *gke-windows-builder_validate*: continuous test that uses the
    gke-windows-builder to build a basic multi-arch Windows container.
*   *gke-windows-builder_validate_aspnet*: continuous test that builds a more
    sophisticated container.
*   *gke-windows-builder_aspnet_manual_test*: manual flow for building a more
    sophisticated container.
*   *gke-windows-builder_release*: builds and tags a release version of the
    gke-windows-builder. See http://go/gke-windows/gke-windows-builder-release.

## Manually

During development you may need to manually build and run the builder from this
repository. To build the builder:

### One-time steps in your GCP project

```shell
export PROJECT=$(gcloud info --format='value(config.project)')
export MEMBER=$(gcloud projects describe $PROJECT --format 'value(projectNumber)')@cloudbuild.gserviceaccount.com

# Enable the Cloud Build and Compute APIs on your project:
gcloud services enable cloudbuild.googleapis.com
gcloud services enable compute.googleapis.com

# Assign roles. These roles are required for the builder to create the Windows
# Server VMs, to copy the workspace to a Cloud Storage bucket and to configure the
# networks to build the Docker image:
gcloud projects add-iam-policy-binding $PROJECT --member=serviceAccount:$MEMBER --role='roles/compute.instanceAdmin'
gcloud projects add-iam-policy-binding $PROJECT --member=serviceAccount:$MEMBER --role='roles/iam.serviceAccountUser'
gcloud projects add-iam-policy-binding $PROJECT --member=serviceAccount:$MEMBER --role='roles/compute.networkViewer'
gcloud projects add-iam-policy-binding $PROJECT --member=serviceAccount:$MEMBER --role='roles/storage.admin'

# Add a firewall rule named allow-winrm-ingress to allow WinRM to connect to
# Windows Server VMs to run a Docker build:
gcloud compute firewall-rules create allow-winrm-ingress --allow=tcp:5986 --direction=INGRESS
```

### Build steps

The "official" build is performed using Cloud Build:

```shell
gcloud builds submit --config=builder/cloudbuild.yaml builder/
```

If the Cloud Build for the gke-windows-builder fails, you'll see ERROR output.
If the build succeeds then after a few minutes you should see output like:

```
...
Successfully tagged gcr.io/PROJECT/gke-windows-builder:latest
PUSH
Pushing gcr.io/PROJECT/gke-windows-builder
...
DONE
--------------------------------------------------------------------------------------------------
ID                                    CREATE_TIME                DURATION  SOURCE                                                                                                   IMAGES                                                          STATUS
96348426-4d88-4edd-a28c-d19229215fee  2021-06-29T23:55:30+00:00  1M20S     gs://PROJECT_cloudbuild/source/1625010929.695731-46d1a97f96db436ba32db698e7ea886e.tgz  gcr.io/PROJECT/gke-windows-builder (+1 more)  SUCCESS
```

If you visit
[gcr.io/PROJECT/gke-windows-builder](http://gcr.io/PROJECT/gke-windows-builder)
you should now see the `latest` builder image that you just built and pushed.

As part of the Cloud Build process you'll see output showing the `go build` step
that builds the Go code you may be modifying. To reduce your dev/build cycle
time, you can copy and modify the command from this step and run it directly
(from the `builder/` subdir), something like:

```shell
GO111MODULE=on CGO_ENABLED=0 go build -o /tmp/go/bin/gke-windows-builder_main
```

### Using the builder you just built (testing your changes)

The gke-windows-builder can now be used to a Windows application container as a
multi-arch container image. You can use the [example](example) application code
from this repository, or you can follow our
[tutorial](https://cloud.google.com/kubernetes-engine/docs/tutorials/building-windows-multi-arch-images#creating_the_helloexe_binary_in_your_workspace)
for building a simple Hello World application container.

Create a Cloud Build configuration file, `cloudbuild.yaml`, in the same
directory as your application container's `Dockerfile`. The configuration file
must have a step that refers to the name of the gke-windows-builder that you
just built, `gcr.io/$PROJECT_ID/gke-windows-builder`. You can add additional
arguments in the configuration file as well - see the [examples](example).

Once you've defined your Cloud Build configuration file, submit it to Cloud
Build:

```shell
gcloud builds submit --config=example/basic/cloudbuild.yaml example/basic/
```

This Windows multi-arch container build will take at least a few minutes to
complete.

# Using the gke-windows-builder released by the GKE team

See our public documentation for
[building multi-arch images using the gke-windows-builder](https://cloud.google.com/kubernetes-engine/docs/tutorials/building-windows-multi-arch-images#building_multi-arch_images_using_the_gke-windows-builder).

// Copyright 2021 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"gke-windows-builder/builder/builder"

	"github.com/masterzen/winrm"
	"google.golang.org/api/googleapi"
)

var (
	projectID               = flag.String("project", "", "The project Id to use when creating the Windows Instance (uses gcloud default if not specified)")
	workspacePath           = flag.String("workspace-path", "/workspace", "The directory to copy data from")
	workspaceBucket         = flag.String("workspace-bucket", "", "The bucket to copy the directory to. Defaults to {project-id}_builder_tmp")
	workspaceBucketLocation = flag.String("workspace-bucket-location", "", "The location of the bucket. Defaults to 'us' which is the GCS API default location'")
	network                 = flag.String("network", "default", "The VPC network to use when creating the Windows Instance (uses 'default' if not specified)")
	networkProject          = flag.String("network-project", "", "The project where the VPC network is located (inferred if not specified).")
	subnetwork              = flag.String("subnetwork", "default", "The Subnetwork name to use when creating the Windows Instance")
	subnetworkProject       = flag.String("subnetwork-project", "", "(deprecated) The project where the Subnetwork is located (uses --network-project instead)")
	region                  = flag.String("region", "us-central1", "The region to create the Windows Instance in (where the Subnetwork is located)")
	zone                    = flag.String("zone", "us-central1-f", "The zone name to use when creating the Windows Instance")
	labels                  = flag.String("labels", "", "List of label KEY=VALUE pairs separated by comma to add when creating the Windows Instance")
	machineType             = flag.String("machineType", "", "The machine type to use when creating the Windows Instance")
	bootDiskType            = flag.String("boot-disk-type", "pd-standard", "Windows instance boot disk type. Default value is pd-standard, other values include pd-ssd and pd-balanced")
	bootDiskSizeGB          = flag.Int64("boot-disk-size-GB", 75, "Instance boot disk size (in GB). Must be at least 40 GB")
	copyTimeout             = flag.Duration("copy-timeout", 5*time.Minute, "The workspace copy timeout in minutes")
	serviceAccount          = flag.String("serviceAccount", "default", "The service account to use when creating the Windows Instance")
	containerImageName      = flag.String("container-image-name", "", "The target container image:tag name")
	pickedVersions          = flag.String("versions", "", "List of Windows Server versions user wants to support. If not provided, the container will be built to support all Windows versions that GKE supports")
	reuseBuilderInstances   = flag.Bool("reuse-builder-instances", false, "Look for existing instances by labels and instance-name-prefix and reuse them for build, create new instance only if none were found. Avoid when queuing parallel builds.")
	instanceNamePrefix      = flag.String("instance-name-prefix", "windows-builder-", "Prefix to use for created GCE instances. Defaults to 'windows-builder-'")
	testObsoleteVersion     = flag.Bool("testonly-test-obsolete-versions", false, "If true, verify the obsolete Windows versions won't fail the builder. For testing purposes only")
	setupTimeout            = flag.Duration("setup-timeout", 20*time.Minute, "Time out to wait for Windows instance to be ready for winrm connection and Docker setup")
	useInternalIP           = flag.Bool("use-internal-ip", false, "Use internal IP addresses (for shared VPCs), also implies no need for firewall rules")
	ExternalIP              = flag.Bool("external-ip", true, "Create external IP addresses for VMs, If false then Cloud NAT must be enabled, see README for details.")
	skipFirewallCheck       = flag.Bool("skip-firewall-check", false, "Skip checking that the project has a firewall rule permitting WinRM ingress")
	dockerfileName          = flag.String("dockerfile", "Dockerfile", "Name of the Dockerfile to use in the build process. Defaults to 'Dockerfile'")
	// Windows version and GCE container image family map
	// Note:
	// 1. Mapping between version <-> image family name, NOT specific image name
	// 2. The version name need to match with servercore container version in Dockerfile file
	versionMap = map[string]string{
		"2004":     "windows-cloud/global/images/family/windows-2004-core",
		"20H2":     "windows-cloud/global/images/family/windows-20h2-core",
		"ltsc2019": "windows-cloud/global/images/family/windows-2019-core",
		"ltsc2022": "windows-cloud/global/images/family/windows-2022-core",
	}
	commandTimeout = 10 * time.Minute
)

type buildArgsArray []string

var buildArgs buildArgsArray

func (i *buildArgsArray) String() string {
	return "my string representation"
}

func (i *buildArgsArray) Set(value string) error {
	*i = append(*i, value)
	return nil
}

// builderServerStatus contains builder server and associated error.
type builderServerStatus struct {
	s   *builder.Server
	err error
}

func main() {
	log.Print("Starting Windows multi-arch container builder")
	flag.Var(&buildArgs, "build-arg", "The list of parameters to pass to the docker build command")
	flag.Parse()
	if *containerImageName == "" {
		log.Fatalf("Error container-image-name flag is required but was not set")
	}

	if *networkProject != "" && *subnetworkProject != "" && *networkProject != *subnetworkProject {
		log.Fatalf("When both network and subnetwork projects are set, they must be identical")
	}
	// subnetworkProject is deprecated. If only subnetwork was set, then copy its value to networkProject
	if *subnetworkProject != "" && *networkProject == "" {
		*networkProject = *subnetworkProject
	}

	pickedVersionMap := getPickedVersionMap(*pickedVersions)
	// Add obsolete 1809 version for test
	if *testObsoleteVersion {
		pickedVersionMap["1809"] = "windows-cloud/global/images/family/windows-1809-core-for-containers"
	}

	var err error
	// Fetch builder project ID from metadata or gcloud command, if it's not set in flags
	if *projectID == "" {
		if *projectID, err = builder.GetProject(); err != nil {
			log.Fatalf("Failed to get builder project ID: %+v", err)
		}
	}

	if *workspaceBucket == "" {
		*workspaceBucket = *projectID + "_builder_tmp"
	}

	if err = setupProjectForBuilder(context.Background()); err != nil {
		log.Fatalf("Failed to setup builder project with error: %+v", err)
	}

	if err = process(pickedVersionMap); err != nil {
		log.Fatalf("Windows multi-arch container building process failed with error: %+v", err)
	}
	log.Println("Windows multi-arch container building process is completed")
}

func setupProjectForBuilder(ctx context.Context) error {
	var err error
	if err = builder.NewGCSBucketIfNotExists(ctx, *projectID, *workspaceBucket, *workspaceBucketLocation); err != nil {
		return fmt.Errorf("Failed creating bucket: %v, with error: %+v", *workspaceBucket, err)
	}

	if *useInternalIP {
		log.Printf("Using a VM without an external IP. Make sure your build is using a worker pool connected to the specified network.")
	}

	if *skipFirewallCheck {
		log.Printf("skipping checks that WinRM firewall rules exist")
		return nil
	}
	return builder.CheckProjectFirewalls(ctx, builder.NewInstanceNetworkConfig(projectID, network, networkProject, subnetwork, region))
}

// Main building process
func process(pickedVersionMap map[string]string) error {
	var bss []builderServerStatus
	defer func() {
		shutdownBuildServers(bss)
	}()

	if err := buildSingleArchContainers(pickedVersionMap, &bss); err != nil {
		return err
	}
	if err := buildMultiArchContainer(pickedVersionMap, bss); err != nil {
		return err
	}
	return nil
}

// Bring up Windows Build Servers & build single-arch containers in parallel
func buildSingleArchContainers(pickedVersionMap map[string]string, bss *[]builderServerStatus) error {
	ch := make(chan builderServerStatus, len(pickedVersionMap))
	wg := sync.WaitGroup{}
	for ver, imageFamily := range pickedVersionMap {
		wg.Add(1)
		go func(ver string, imageFamily string) {
			defer wg.Done()
			ctx := context.Background()
			ch <- buildSingleArchContainer(ctx, ver, imageFamily)
		}(ver, imageFamily)
	}
	// Wait until all builder server statuses returned.
	wg.Wait()
	chLen := len(ch)
	if chLen != len(pickedVersionMap) {
		return fmt.Errorf("Unexpected discrepancy happened, the number of builder server statuses in channel is not equal to length of pickedVersionMap")
	}
	for i := 0; i < chLen; i++ {
		*bss = append(*bss, <-ch)
	}
	// If any fatal error happens, exit the process
	for _, bs := range *bss {
		if bs.err != nil {
			return fmt.Errorf("Error happened when building single-arch containers: %+v", bs.err)
		}
	}
	return nil
}

// Build multi-arch container on any available server.
// If the pickedVersionMap has obsolete image version, it's still working fine, as `docker manifest create` command is resilient for non-existing containers.
// E.g. `docker manifest create container container_1909 container_2019` works if container_1909 doesn't exist. The resulting multi-arch container will have the only manifest of container_2019.
func buildMultiArchContainer(pickedVersionMap map[string]string, bss []builderServerStatus) error {
	var isManifestCreated bool
	for _, bs := range bss {
		if bs.s != nil && !isManifestCreated {
			manifestCreateCmdArgs := constructArgsOfManifestCreateCommand(pickedVersionMap)
			err := createMultiArchContainerOnRemote(&bs.s.RemoteWindowsServer, *containerImageName, manifestCreateCmdArgs, commandTimeout)
			if err != nil {
				log.Printf("Error executing createMultiArchContainerOnRemote on instance: %v, with error: %+v", *bs.s.RemoteWindowsServer.Hostname, err)
			} else {
				isManifestCreated = true
			}
		}
	}
	if !isManifestCreated {
		return fmt.Errorf("Failed to create the final multi-arch manifest")
	}
	return nil
}

func shutdownBuildServers(bss []builderServerStatus) {
	if *reuseBuilderInstances {
		log.Printf("Keeping instances for reuse")
		wg := sync.WaitGroup{}
		for _, bsc := range bss {
			if bsc.s != nil {
				wg.Add(1)
				go func(bsc builderServerStatus) {
					defer wg.Done()
					bsc.s.RemoteWindowsServer.CleanFolder()
				}(bsc)
			}
		}
		wg.Wait()
		return
	}

	log.Printf("Deleting created instances")
	wg := sync.WaitGroup{}
	for _, bsc := range bss {
		if bsc.s != nil {
			wg.Add(1)
			go func(bsc builderServerStatus) {
				defer wg.Done()
				bsc.s.DeleteInstance()
			}(bsc)
		}
	}
	wg.Wait()
}

// Brings up a Windows Server Instance, build single-arch container and return the buider status.
// If that status's err is nil, the server is still running.
// If err is non-nil, then the server has been stopped.
// So please be aware of cleaning up the running instances after calling this function.
func buildSingleArchContainer(ctx context.Context, ver string, imageFamily string) builderServerStatus {
	var s *builder.Server
	var err error

	netConfig := builder.NewInstanceNetworkConfig(projectID, network, networkProject, subnetwork, region)
	bsc := &builder.WindowsBuildServerConfig{
		InstanceNamePrefix: instanceNamePrefix,
		ImageVersion:       &ver,
		ImageURL:           &imageFamily,
		Zone:               zone,
		NetworkConfig:      netConfig,
		Labels:             labels,
		MachineType:        machineType,
		BootDiskType:       bootDiskType,
		BootDiskSizeGB:     *bootDiskSizeGB,
		ServiceAccount:     serviceAccount,
		UseInternalIP:      *useInternalIP,
		ExternalNAT:        *ExternalIP,
		ReuseInstance:      *reuseBuilderInstances,
	}

	if *reuseBuilderInstances {
		log.Printf("Looking for an exiting %s instance to reuse", ver)
		s, err = builder.FindExistingInstance(ctx, bsc, *projectID)
	}

	if s == nil {
		s, err = builder.NewServer(ctx, bsc, *projectID)
		if err != nil {
			if isImageNotFoundErr(err, imageFamily) {
				log.Printf("Failed to create Windows %[1]s instance, it may be expired, so skip it to continue without stamping Windows %[1]s manifest", ver)
				return builderServerStatus{nil, nil}
			}
			return builderServerStatus{nil, err}
		}
	}

	r := &s.RemoteWindowsServer

	log.Printf("Waiting for Windows %s instance: %s (%s) to become available", ver, *r.Hostname, s.GetInstanceName())
	err = r.WaitForServerBeReady(*setupTimeout)
	if err != nil {
		log.Printf("Error setup Windows %s instance: %s with error: %+v", ver, *r.Hostname, err)
		return builderServerStatus{s, err}
	}

	r.WorkspaceBucket = workspaceBucket
	// Copy workspace to remote machine
	log.Printf("Copying local workspace to remote machine: %v", *r.Hostname)
	err = r.Copy(*workspacePath, *copyTimeout)
	if err != nil {
		log.Printf("Error copying workspace to %v : %+v", *r.Hostname, err)
		return builderServerStatus{s, err}
	}

	err = buildSingleArchContainerOnRemote(r, *containerImageName, ver, commandTimeout)
	if err != nil {
		log.Printf("Error building single arch container on remote %v : %+v", *r.Hostname, err)
		return builderServerStatus{s, err}
	}
	return builderServerStatus{s, nil}
}

// Get the version map for picked versions
// If picked versions are empty, get the default full version map.
func getPickedVersionMap(pickedVersions string) map[string]string {
	var pickedVersionMap = map[string]string{}
	// If picked versions flag is not set, use the default full version map.
	if pickedVersions == "" {
		return versionMap
	}
	vers := strings.Split(pickedVersions, ",")
	for _, ver := range vers {
		ver = strings.TrimSpace(ver)
		if ver != "" {
			if versionMap[ver] == "" {
				log.Fatalf("picked-versions flag has unsupported Windows Server versions: %s", ver)
			}
			pickedVersionMap[ver] = versionMap[ver]
		}
	}
	if len(pickedVersionMap) == 0 {
		log.Fatalf("no supported Windows Server versions found")
	}
	return pickedVersionMap
}

// Check if the error is image not found error.
func isImageNotFoundErr(err error, imageFamily string) bool {
	var gceAPIErr *googleapi.Error
	if errors.As(err, &gceAPIErr) {
		// Image not found error sample:
		// googleapi: Error 404: The resource 'projects/windows-cloud/global/images/family/windows-1809-core-for-containers' was not found
		if gceAPIErr.Code == 404 && strings.Contains(gceAPIErr.Message, imageFamily) {
			return true
		}
	}
	return false
}

// Construct the args of `docker manifest create` cmd
// e.g. `docker manifest create demo:cloudbuild demo:cloudbuild_ltsc2019 demo:cloudbuild_1909`
func constructArgsOfManifestCreateCommand(pickedVersionMap map[string]string) string {
	args := *containerImageName
	for ver := range pickedVersionMap {
		args += fmt.Sprint(" ", *containerImageName, "_", ver)
	}
	return args
}

func buildSingleArchContainerOnRemote(
	r *builder.RemoteWindowsServer,
	containerImageName string,
	version string,
	timeout time.Duration,
) error {
	registry := strings.Split(containerImageName, "/")[0]
	if registry == "gcr.io" {
		registry = ""
	}
	buildargs := ""
	for _, arg := range buildArgs {
		buildargs += "--build-arg " + arg + " "
	}
	buildSingleArchContainerScript := fmt.Sprintf(`
	$env:DOCKER_CLI_EXPERIMENTAL = 'enabled'
	gcloud auth --quiet configure-docker %[3]s
	docker build -t %[1]s_%[2]s --build-arg WINDOWS_VERSION=%[2]s -f %[4]s %[5]s .
	docker push %[1]s_%[2]s
	`, containerImageName, version, registry, *dockerfileName, buildargs)

	log.Printf("Start to build single-arch container with commands: %s", buildSingleArchContainerScript)
	return r.RunCommand(winrm.Powershell(buildSingleArchContainerScript), *r.WorkspaceFolder, timeout)
}

// This function assumes that the remote server has already performed gcloud docker authentication.
// https://cloud.google.com/artifact-registry/docs/docker/authentication#gcloud-helper
func createMultiArchContainerOnRemote(
	r *builder.RemoteWindowsServer,
	containerImageName string,
	manifestCreateCmdArgs string,
	timeout time.Duration,
) error {
	createMultiarchContainerScript := fmt.Sprintf(`
	$env:DOCKER_CLI_EXPERIMENTAL = 'enabled'
	docker manifest create %s
	docker manifest push %s
	`, manifestCreateCmdArgs, containerImageName)

	log.Printf("Start to create multi-arch container with commands: %s", createMultiarchContainerScript)
	return r.RunCommand(winrm.Powershell(createMultiarchContainerScript), *r.WorkspaceFolder, timeout)
}

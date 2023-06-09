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

package builder

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	random "math/rand"
	"os/exec"
	"strings"
	"text/template"
	"time"

	"github.com/pborman/uuid"

	"cloud.google.com/go/compute/metadata"
	"golang.org/x/oauth2/google"
	compute "google.golang.org/api/compute/v1"
)

const (
	computeUrlPrefix = "https://www.googleapis.com/compute/v1/projects/"
)

// Setup the Winrm, disable the Windows Defender, install the docker if needed
// Note: it'll restart the instance to make it effective
var (
	setupScriptPS1Template = template.Must(template.New("windows-startup-script-ps1").Parse(`
# Disable Windows Defender service
# Windows Defender may scan the C:\ProgramData\Docker\ folder, make it locked from docker build.
# https://github.com/docker/for-win/issues/2117
if ((Get-WindowsFeature -Name 'Windows-Defender').Installed) {
	Write-Host "Disabling Windows Defender service"
	Set-MpPreference -DisableRealtimeMonitoring $true
	Uninstall-WindowsFeature -Name 'Windows-Defender'
	Restart-Computer -Force
}

# Writes $Message to the console. Terminates the script if $Fatal is set.
function Test-ContainersFeatureInstalled {
	return (Get-WindowsFeature Containers).Installed
}
# After this function returns, the computer must be restarted to complete
# the installation!
function Install-ContainersFeature {
	Write-Host "Installing Windows 'Containers' feature"
	Install-WindowsFeature Containers
}
function Test-DockerIsInstalled {
	return ((Get-Package -ProviderName DockerMsftProvider -ErrorAction SilentlyContinue | Where-Object Name -eq 'docker') -ne $null)
}
function Test-DockerIsRunning {
	return ((Get-Service docker).Status -eq 'Running')
}
# Installs Docker EE via the DockerMsftProvider. Ensure that the Windows
# Containers feature is installed before calling this function; otherwise,
# a restart may be needed after this function returns.
function Install-Docker {
	Write-Host 'Installing NuGet module'
	Install-PackageProvider -Name NuGet -MinimumVersion 2.8.5.201 -Force
	Write-Host 'Installing DockerMsftProvider module'
	Install-Module -Name DockerMsftProvider -Repository PSGallery -Force
	Write-Host "Installing latest Docker EE version"
	Install-Package -Name docker -ProviderName DockerMsftProvider -Force -Verbose
}
{{if .AllowNondistributableArtifacts}}
function Set-DockerAllowNondistributableArtifacts {
	Write-Host 'Configuring Docker to push nondistributable artifacts to {{.AllowNondistributableArtifacts}}'
	if (!(Test-Path 'C:\ProgramData\docker\config\daemon.json'))
	{
		 New-Item -Force -Path 'C:\ProgramData\docker\config' -Name 'daemon.json' -Type 'file' -Value '{}'
	}
	$config = Get-Content 'C:\ProgramData\docker\config\daemon.json' -raw | ConvertFrom-Json
	$config | Add-Member -NotePropertyName 'allow-nondistributable-artifacts' -NotePropertyValue @('{{.AllowNondistributableArtifacts}}')
	$config | ConvertTo-Json -depth 32 | Set-Content 'C:\ProgramData\docker\config\daemon.json'
}
{{end}}
if (-not (Test-ContainersFeatureInstalled)) {
	Install-ContainersFeature
	Write-Host 'Restarting computer after enabling Windows Containers feature'
	Restart-Computer -Force
	# Restart-Computer does not stop the rest of the script from executing.
	exit 0
}
if (-not (Test-DockerIsInstalled)) {
	Install-Docker
}
{{if .AllowNondistributableArtifacts}}
Set-DockerAllowNondistributableArtifacts
{{end}}
# For some reason the docker service may not be started automatically on the
# first reboot, although it seems to work fine on subsequent reboots.
Restart-Service docker
Start-Sleep 5
if (-not (Test-DockerIsRunning)) {
	throw "docker service failed to start or stay running"
}

# Setup Winrm
winrm set winrm/config/service/auth '@{Basic="true"}'

Write-Host 'Windows instance setup is completed'
`))
)

// Server encapsulates a GCE Instance.
type Server struct {
	context   *context.Context
	projectID string
	zone      string
	service   *compute.Service
	instance  *compute.Instance
	RemoteWindowsServer
}

// getProject gets the project ID.
func GetProject() (string, error) {
	// Get projectID from GCE metadata.
	if metadata.OnGCE() {
		// Use the GCE Metadata service.
		projectID, err := metadata.ProjectID()
		if err != nil {
			return "", fmt.Errorf("Failed to get project ID from instance metadata with error: %+v", err)
		}
		return projectID, nil
	}
	// Shell out to gcloud.
	cmd := exec.Command("gcloud", "config", "get-value", "project")
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("Failed to shell out to gcloud: %+v", err)
	}
	projectID := strings.TrimSuffix(out.String(), "\n")
	return projectID, nil
}

// NewServer creates a new Windows server on GCE.
func NewServer(ctx context.Context, bs *WindowsBuildServerConfig, projectID string) (*Server, error) {
	s := &Server{projectID: projectID, zone: *bs.Zone}
	var err error
	if err = s.newGCEService(ctx); err != nil {
		log.Printf("Failed to start GCE service to create servers: %+v", err)
		return nil, err
	}
	if err = s.newInstance(bs); err != nil {
		log.Printf("Failed to start Windows VM: %+v", err)
		return nil, err
	}
	err = s.resetPasswordAndPopulateRemoteServer(bs.UseInternalIP)
	if err != nil {
		return nil, err
	}

	return s, nil
}

func existingServer(ctx context.Context, zone string, projectID string, name string, useInternalIP bool) (*Server, error) {
	s := &Server{projectID: projectID, zone: zone}
	var err error
	if err = s.newGCEService(ctx); err != nil {
		log.Printf("Failed to start GCE service to create servers: %+v", err)
		return nil, err
	}
	if err = s.existingInstance(name); err != nil {
		log.Printf("Failed to start Windows VM: %+v", err)
		return nil, err
	}

	err = s.resetPasswordAndPopulateRemoteServer(useInternalIP)
	if err != nil {
		return nil, err
	}

	return s, nil
}

func FindExistingInstance(ctx context.Context, bs *WindowsBuildServerConfig, projectID string) (*Server, error) {
	s := &Server{projectID: projectID, zone: *bs.Zone}
	var err error
	if err = s.newGCEService(ctx); err != nil {
		log.Printf("Failed to start GCE service to create servers: %+v", err)
		return nil, err
	}

	instanceList, err := s.service.Instances.
		List(projectID, *bs.Zone).
		Filter(buildListInstancesFilter(bs.GetLabelsMap(), bs.InstanceNamePrefix)).
		Do()

	if err != nil {
		log.Printf("Failed to list relevant instances: %v", err)
		return nil, err
	}

	if len(instanceList.Items) == 0 {
		log.Printf("Found no relevant instances")
		return nil, nil
	}

	random.Seed(time.Now().Unix())
	chosenInstance := instanceList.Items[random.Intn(len(instanceList.Items))]

	log.Printf("Found %d relevant instances for version: %s, chose %s", len(instanceList.Items), *bs.ImageVersion, chosenInstance.Name)

	return existingServer(ctx, *bs.Zone, projectID, chosenInstance.Name, bs.UseInternalIP)
}

func buildListInstancesFilter(labels map[string]string, instanceNamePrefix *string) string {
	filters := []string{"(status eq RUNNING)"}

	if instanceNamePrefix != nil {
		filters = append(filters, fmt.Sprintf("(name eq %s.*)", *instanceNamePrefix))
	}

	for labelKey, value := range labels {
		filters = append(filters, fmt.Sprintf("(labels.%s eq %s)", labelKey, value))
	}

	return strings.Join(filters, " ")
}

func newGCEService(ctx context.Context) (*compute.Service, error) {
	client, err := google.DefaultClient(ctx, compute.ComputeScope)
	if err != nil {
		log.Printf("Failed to create Google Default Client: %v", err)
		return nil, err
	}
	service, err := compute.New(client)
	if err != nil {
		log.Printf("Failed to create Compute Service: %v", err)
		return nil, err
	}
	return service, nil
}

// newGCEService creates a new Compute service.
func (s *Server) newGCEService(ctx context.Context) error {
	service, err := newGCEService(ctx)
	s.service = service
	return err
}

// newInstance starts a Windows VM on GCE and returns host, username, password.
func (s *Server) newInstance(bs *WindowsBuildServerConfig) error {
	name := *bs.InstanceNamePrefix + uuid.New()

	machineType := *bs.MachineType
	if machineType == "" {
		machineType = "e2-standard-2"
	}

	accessConfigs := []*compute.AccessConfig{
		{
			Type: "ONE_TO_ONE_NAT",
			Name: "External NAT",
		},
	}

	if !bs.ExternalNAT {
		accessConfigs = nil
	}

	var setupScriptPS1Bytes bytes.Buffer

	data := struct {
		AllowNondistributableArtifacts *string
	}{
		AllowNondistributableArtifacts: bs.AllowNondistributableArtifacts,
	}

	if err := setupScriptPS1Template.Execute(&setupScriptPS1Bytes, data); err != nil {
		log.Printf("Unable to template startup script: %v", err)
		return err
	}

	setupScriptPS1 := setupScriptPS1Bytes.String()

	// https://cloud.google.com/compute/docs/reference/rest/v1/instances#resource:-instance
	instance := &compute.Instance{
		Name:        name,
		MachineType: computeUrlPrefix + s.projectID + "/zones/" + s.zone + "/machineTypes/" + machineType,
		Disks: []*compute.AttachedDisk{
			{
				AutoDelete: true,
				Boot:       true,
				Type:       "PERSISTENT",
				InitializeParams: &compute.AttachedDiskInitializeParams{
					DiskName:    fmt.Sprintf("%s-pd", name),
					SourceImage: computeUrlPrefix + *bs.ImageURL,
					DiskType:    computeUrlPrefix + s.projectID + "/zones/" + s.zone + "/diskTypes/" + *bs.BootDiskType,
					DiskSizeGb:  bs.BootDiskSizeGB,
				},
			},
		},
		Metadata: &compute.Metadata{
			Items: []*compute.MetadataItems{
				&compute.MetadataItems{
					Key:   "windows-startup-script-ps1",
					Value: &setupScriptPS1,
				},
			},
		},
		NetworkInterfaces: []*compute.NetworkInterface{
			&compute.NetworkInterface{
				AccessConfigs: accessConfigs,
			},
		},
		ServiceAccounts: []*compute.ServiceAccount{
			{
				Email: bs.GetServiceAccountEmail(s.projectID),
				Scopes: []string{
					compute.CloudPlatformScope,
				},
			},
		},
		Labels: bs.GetLabelsMap(),
	}

	networkUrl, subnetUrl := InstanceNetworkUrls(bs.NetworkConfig, s.projectID)
	if networkUrl != "" {
		instance.NetworkInterfaces[0].Network = networkUrl
	}
	if subnetUrl != "" {
		instance.NetworkInterfaces[0].Subnetwork = subnetUrl
	}

	op, err := s.service.Instances.Insert(s.projectID, s.zone, instance).Do()
	if err != nil {
		log.Printf("GCE Instances insert call failed: %v", err)
		return err
	}
	err = s.waitForComputeOperation(op)
	if err != nil {
		log.Printf("Wait for instance start failed: %v", err)
		return err
	}

	etag := op.Header.Get("Etag")
	inst, err := s.service.Instances.Get(s.projectID, s.zone, name).IfNoneMatch(etag).Do()
	if err != nil {
		log.Printf("Could not get GCE Instance details after creation: %v", err)
		return err
	}
	log.Printf("Successfully created instance: %s, version: %s", inst.Name, *bs.ImageVersion)
	s.instance = inst
	return nil
}

func (s *Server) existingInstance(name string) error {
	inst, err := s.service.Instances.Get(s.projectID, s.zone, name).Do()
	if err != nil {
		log.Printf("Could not get provided existing GCE Instance details: %v", err)
		return err
	}
	log.Printf("Successfully retrieved instance: %s", inst.Name)
	s.instance = inst
	return nil
}

// refreshInstance refreshes latest info from GCE into struct.
func (s *Server) refreshInstance() error {
	inst, err := s.service.Instances.Get(s.projectID, s.zone, s.instance.Name).Do()
	if err != nil {
		log.Printf("Could not refresh instance: %v", err)
		return err
	}
	s.instance = inst
	return nil
}

// DeleteInstance stops a Windows VM on GCE.
func (s *Server) DeleteInstance() {
	_, err := s.service.Instances.Delete(s.projectID, s.zone, s.instance.Name).Do()
	if err != nil {
		log.Printf("Could not delete instance: %s, with error: %v", *s.RemoteWindowsServer.Hostname, err)
	}
	log.Printf("Instance: %s shut down successfully", *s.RemoteWindowsServer.Hostname)
}

func (s *Server) GetInstanceName() string {
	if s.instance == nil {
		return ""
	}

	return s.instance.Name
}

func (s *Server) resetPasswordAndPopulateRemoteServer(useInternalIP bool) error {
	// Reset password
	username := "builder"
	password, err := s.resetWindowsPassword(username)
	if err != nil {
		log.Printf("Failed to reset Windows password: %+v", err)
		return err
	}
	// Get IP address.
	ip, err := s.getIP(useInternalIP)
	if err != nil {
		log.Printf("Failed to get IP address: %+v", err)
		return err
	}

	workspaceFolder := fmt.Sprintf(`C:\ws-%s`, uuid.New())

	// Set and return Remote.
	s.RemoteWindowsServer = RemoteWindowsServer{
		Hostname:        &ip,
		Username:        &username,
		Password:        &password,
		WorkspaceFolder: &workspaceFolder,
	}

	return nil
}

// getIP gets the IP for an instance (external or internal if using shared VPCs).
func (s *Server) getIP(useInternalIP bool) (string, error) {
	err := s.refreshInstance()
	if err != nil {
		log.Printf("Error refreshing instance: %+v", err)
	}
	for _, ni := range s.instance.NetworkInterfaces {
		if useInternalIP {
			return ni.NetworkIP, nil
		}
		for _, ac := range ni.AccessConfigs {
			if ac.Name == "External NAT" {
				return ac.NatIP, nil
			}
		}
	}
	return "", errors.New("Could not get external NAT IP from list")
}

// WindowsPasswordConfig stores metadata to be sent to GCE.
type WindowsPasswordConfig struct {
	key      *rsa.PrivateKey
	password string
	UserName string    `json:"userName"`
	Modulus  string    `json:"modulus"`
	Exponent string    `json:"exponent"`
	Email    string    `json:"email"`
	ExpireOn time.Time `json:"expireOn"`
}

// WindowsPasswordResponse stores data received from GCE.
type WindowsPasswordResponse struct {
	UserName          string `json:"userName"`
	PasswordFound     bool   `json:"passwordFound"`
	EncryptedPassword string `json:"encryptedPassword"`
	Modulus           string `json:"modulus"`
	Exponent          string `json:"exponent"`
	ErrorMessage      string `json:"errorMessage"`
}

// resetWindowsPassword securely resets the admin Windows password.
// See https://cloud.google.com/compute/docs/instances/windows/automate-pw-generation
func (s *Server) resetWindowsPassword(username string) (string, error) {
	//Create random key and encode
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		log.Printf("Failed to generate random RSA key: %v", err)
		return "", err
	}
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, uint32(key.E))
	wpc := WindowsPasswordConfig{
		key:      key,
		UserName: username,
		Modulus:  base64.StdEncoding.EncodeToString(key.N.Bytes()),
		Exponent: base64.StdEncoding.EncodeToString(buf[1:]),
		Email:    "nobody@nowhere.com",
		ExpireOn: time.Now().Add(time.Minute * 5),
	}
	data, err := json.Marshal(wpc)
	dstring := string(data)
	if err != nil {
		log.Printf("Failed to marshal JSON: %v", err)
		return "", err
	}

	//Write key to instance metadata and wait for op to complete
	log.Print("Writing Windows instance metadata for password reset")
	var found bool
	for _, mdi := range s.instance.Metadata.Items {
		if mdi.Key == "windows-keys" {
			log.Print("Altering current key")

			mdi.Value = &dstring
			found = true
			break
		}
	}

	if !found {
		s.instance.Metadata.Items = append(s.instance.Metadata.Items, &compute.MetadataItems{Key: "windows-keys", Value: &dstring})
	}

	op, err := s.service.Instances.SetMetadata(s.projectID, s.zone, s.instance.Name, &compute.Metadata{
		Fingerprint: s.instance.Metadata.Fingerprint,
		Items:       s.instance.Metadata.Items,
	}).Do()
	if err != nil {
		log.Printf("Failed to set instance metadata: %v", err)
		return "", err
	}
	err = s.waitForComputeOperation(op)
	if err != nil {
		log.Printf("Compute operation timed out")
		return "", err
	}

	//Read and decode password
	log.Print("Waiting for Windows password response")
	timeout := time.Now().Add(time.Minute * 5)
	hash := sha1.New()
	for time.Now().Before(timeout) {
		output, err := s.service.Instances.GetSerialPortOutput(s.projectID, s.zone, s.instance.Name).Port(4).Do()
		if err != nil {
			log.Printf("Unable to get serial port output: %v", err)
			return "", err
		}
		responses := strings.Split(output.Contents, "\n")
		for _, response := range responses {
			var wpr WindowsPasswordResponse
			if err := json.Unmarshal([]byte(response), &wpr); err != nil {
				log.Printf("Cannot Unmarshal password: %v", err)
				continue
			}
			if wpr.Modulus == wpc.Modulus {
				decodedPassword, err := base64.StdEncoding.DecodeString(wpr.EncryptedPassword)
				if err != nil {
					log.Printf("Cannot Base64 decode password: %v", err)
					return "", err
				}
				password, err := rsa.DecryptOAEP(hash, rand.Reader, wpc.key, decodedPassword, nil)
				if err != nil {
					log.Printf("Cannot decrypt password response: %v", err)
					return "", err
				}
				return string(password), nil
			}
		}
		time.Sleep(2 * time.Second)
	}
	err = errors.New("Could not retrieve password before timeout")
	return "", err
}

// waitForComputeOperation waits for a compute operation
func (s *Server) waitForComputeOperation(op *compute.Operation) error {
	log.Printf("Waiting for %+v to complete", op.Name)
	timeout := time.Now().Add(300 * time.Second)
	for time.Now().Before(timeout) {
		newop, err := s.service.ZoneOperations.Get(s.projectID, s.zone, op.Name).Do()
		if err != nil {
			log.Printf("Failed to update operation status: %v", err)
			return err
		}
		if newop.Status == "DONE" {
			if newop.Error == nil || len(newop.Error.Errors) == 0 {
				return nil
			}
			//Operation Error
			for _, opError := range newop.Error.Errors {
				fmt.Printf("Operation Error. Code: %s, Location: %s, Message: %s :", opError.Code, opError.Location, opError.Message)
			}
			return fmt.Errorf("Compute operation %s completed with errors", op.Name)
		}
		time.Sleep(1 * time.Second)
	}
	err := fmt.Errorf("Compute operation %s timed out", op.Name)
	return err
}

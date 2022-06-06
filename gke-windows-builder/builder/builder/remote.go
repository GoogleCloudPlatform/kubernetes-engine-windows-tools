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
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"github.com/masterzen/winrm"
	"github.com/packer-community/winrmcp/winrmcp"
)

// RemoteWindowsServer represents a remote Windows server.
type RemoteWindowsServer struct {
	Hostname        *string
	Username        *string
	Password        *string
	WorkspaceBucket *string
	WorkspaceFolder *string
}

// WindowsBuildServerConfig stores the configs of windows build server.
type WindowsBuildServerConfig struct {
	InstanceNamePrefix *string
	ImageURL           *string
	Zone               *string
	NetworkConfig      *InstanceNetworkConfig
	Labels             *string
	MachineType        *string
	ServiceAccount     *string
	BootDiskType       *string
	BootDiskSizeGB     int64
	UseInternalIP      bool
	ExternalNAT        bool
}

// Wait for server to be available for Winrm connection and Docker setup.
func (r *RemoteWindowsServer) WaitForServerBeReady(setupTimeout time.Duration) error {
	log.Printf("Waiting at most %+v for WinRM connection and Docker to be available.", setupTimeout)
	timeout := time.Now().Add(setupTimeout)
	for time.Now().Before(timeout) {
		err := r.RunCommand("docker -v", *r.WorkspaceFolder, setupTimeout)
		if err == nil {
			return nil
		}
		time.Sleep(10 * time.Second)
	}
	return fmt.Errorf("Timed out waiting for server to be available for WinRM connection and Docker within %v", setupTimeout)
}

// Copy workspace from Linux to Windows.
func (r *RemoteWindowsServer) Copy(inputPath string, copyTimeout time.Duration) error {
	defer func() {
		// Flush stdout
		fmt.Println()
	}()

	if copyTimeout <= 0 {
		return errors.New("copy timeout must be greater than 0")
	}

	hostport := fmt.Sprintf("%s:5986", *r.Hostname)
	c, err := winrmcp.New(hostport, &winrmcp.Config{
		Auth:                  winrmcp.Auth{User: *r.Username, Password: *r.Password},
		Https:                 true,
		Insecure:              true,
		TLSServerName:         "",
		CACertBytes:           nil,
		OperationTimeout:      copyTimeout,
		MaxOperationsPerShell: 15,
	})
	if err != nil {
		log.Printf("Error creating connection to remote for copy: %+v", err)
		return err
	}

	// First try to create a bucket and have the Windows VM download it via a
	// GS URL. If that fails, use the remote copy method.
	err = r.copyViaBucket(
		context.Background(),
		inputPath,
		copyTimeout,
	)
	if err == nil {
		// Successfully copied via GCE bucket
		log.Printf("Successfully copied data via GCE bucket to %s", *r.WorkspaceFolder)
		return nil
	}

	log.Printf("Failed to copy data via GCE bucket: %v", err)

	err = c.Copy(inputPath, *r.WorkspaceFolder)
	if err != nil {
		log.Printf("Error copying workspace to remote: %+v", err)
		return err
	}

	return nil
}

func (r *RemoteWindowsServer) CleanFolder() error {
	log.Printf("Instance: %s cleaning up workspace folder: %s", *r.Hostname, *r.WorkspaceFolder)

	pwrScript := fmt.Sprintf(`
$ErrorActionPreference = "Stop"
$ProgressPreference = 'SilentlyContinue'
Remove-Item -Path %s -Recurse -Force
`, *r.WorkspaceFolder)

	// Now tell the Windows VM to download it.
	return r.RunCommand(winrm.Powershell(pwrScript), "C:\\", 30*time.Second)
}

func (r *RemoteWindowsServer) copyViaBucket(ctx context.Context, inputPath string, copyTimeout time.Duration) error {
	object := fmt.Sprintf("windows-builder-%d", time.Now().UnixNano())

	gsURL, err := writeZipToBucket(
		ctx,
		*r.WorkspaceBucket,
		object,
		inputPath,
	)
	if err != nil {
		return err
	}

	pwrScript := fmt.Sprintf(`
$ErrorActionPreference = "Stop"
$ProgressPreference = 'SilentlyContinue'
gsutil cp %q %s.zip
Expand-Archive -Path %s.zip -DestinationPath %s -Force
Remove-Item -Path %s.zip -Force
`, gsURL, *r.WorkspaceFolder, *r.WorkspaceFolder, *r.WorkspaceFolder, *r.WorkspaceFolder)

	// Now tell the Windows VM to download it.
	return r.RunCommand(winrm.Powershell(pwrScript), *r.WorkspaceFolder, copyTimeout)
}

// Run command against Windows Server thru WinRM within specific timeout
func (r *RemoteWindowsServer) RunCommand(command string, path string, runTimeout time.Duration) error {
	if runTimeout <= 0 {
		return errors.New("runTimeout must be greater than 0")
	}

	cmdstring := fmt.Sprintf(`cd %s & %s`, path, command)
	endpoint := winrm.NewEndpoint(*r.Hostname, 5986, true, true, nil, nil, nil, runTimeout)
	w, err := winrm.NewClient(endpoint, *r.Username, *r.Password)
	if err != nil {
		return err
	}
	shell, err := w.CreateShell()
	if err != nil {
		return err
	}
	var cmd *winrm.Command
	cmd, err = shell.Execute(cmdstring)
	if err != nil {
		return err
	}

	go io.Copy(os.Stdout, cmd.Stdout)
	go io.Copy(os.Stderr, cmd.Stderr)

	cmd.Wait()
	shell.Close()

	if cmd.ExitCode() != 0 {
		return fmt.Errorf("command failed with exit-code:%d", cmd.ExitCode())
	}

	return nil
}

func (bs *WindowsBuildServerConfig) GetServiceAccountEmail(projectID string) string {
	if *bs.ServiceAccount == "default" || strings.Contains(*bs.ServiceAccount, "@") {
		return *bs.ServiceAccount
	}
	//add service account email suffix
	return fmt.Sprintf("%s@%s.iam.gserviceaccount.com", *bs.ServiceAccount, projectID)
}

func (bs *WindowsBuildServerConfig) GetLabelsMap() map[string]string {
	if *bs.Labels == "" {
		return nil
	}

	var labelsMap map[string]string

	for _, label := range strings.Split(*bs.Labels, ",") {
		labelSpl := strings.Split(label, "=")
		if len(labelSpl) != 2 {
			log.Printf("Error: Label needs to be key=value template. %s label ignored", label)
			continue
		}

		var key = strings.TrimSpace(labelSpl[0])
		if len(key) == 0 {
			log.Printf("Error: Label key can't be empty. %s label ignored", label)
			continue
		}
		var value = strings.TrimSpace(labelSpl[1])

		if labelsMap == nil {
			labelsMap = make(map[string]string)
		}
		labelsMap[key] = value
	}
	return labelsMap
}

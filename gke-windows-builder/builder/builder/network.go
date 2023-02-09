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
	"fmt"
	"log"

	"google.golang.org/api/compute/v1"
)

// InstanceNetworkConfig stores configuration information about the network
// a GCE instance uses.
type InstanceNetworkConfig struct {
	Network        *string
	NetworkProject *string
	Subnet         *string
	SubnetProject  *string
	Region         *string
}

// NewInstanceNetworkConfig returns a pointer to a new InstanceNetworkConfig
// struct whose fields have been set correctly based on the flag values passed
// as args.
func NewInstanceNetworkConfig(instanceProject *string, network *string, networkProject *string, subnet *string, subnetProject *string, region *string) *InstanceNetworkConfig {
	netConfig := InstanceNetworkConfig{
		Network:        network,
		NetworkProject: networkProject,
		Subnet:         subnet,
		SubnetProject:  subnetProject,
		Region:         region,
	}
	if usingSharedVPC(&netConfig) {
		// When Shared VPC is detected, we do not make any assumptions
		// about the networks / projects other than what the user
		// passed as args.
		return &netConfig
	}

	if *netConfig.NetworkProject == "" {
		netConfig.NetworkProject = instanceProject
	}
	if *netConfig.SubnetProject == "" {
		netConfig.SubnetProject = instanceProject
	}
	return &netConfig
}

func usingSharedVPC(netConfig *InstanceNetworkConfig) bool {
	if *netConfig.SubnetProject != "" && *netConfig.NetworkProject == "" {
		// If --subnetwork-project was specified but --network-project
		// was not, this indicates that the user is specifying a Shared
		// VPC subnet and we should let the --network be inferred (this
		// is what the Cloud Console does when using Shared VPC).
		return true
	}
	return false
}

// ProjectNetworkUrls returns urls for referencing the network
// in the InstanceNetworkConfig as global project-level resources.
func ProjectNetworkUrls(netConfig *InstanceNetworkConfig, instanceProject string) string {
	var networkUrl string

	networkUrl = computeUrlPrefix + *netConfig.NetworkProject + "/global/networks/" + *netConfig.Network
	return networkUrl
}

// InstanceNetworkUrls returns urls for referencing the network and subnetwork
// in the InstanceNetworkConfig during instance creation. The network url will
// be a global resource and the subnet url will be a regional resource.  If an
// empty url string is returned then it does not need to be specified during
// instance creation and it will be inferred by the GCE API.
func InstanceNetworkUrls(netConfig *InstanceNetworkConfig, instanceProject string) (string, string) {
	var networkUrl, subnetUrl string
	subnetUrl = computeUrlPrefix + *netConfig.SubnetProject + "/regions/" + *netConfig.Region + "/subnetworks/" + *netConfig.Subnet

	if usingSharedVPC(netConfig) {
		log.Printf("Detected Shared VPC scenario, subnet will be specified and network will be inferred")
		return networkUrl, subnetUrl
	}

	networkUrl = computeUrlPrefix + *netConfig.SubnetProject + "/global/networks/" + *netConfig.Network
	return networkUrl, subnetUrl
}

// CheckProjectFirewalls verifies that the projects in the
// InstanceNetworkConfig have the necessary firewall rules configured for
// controlling the builder VMs. Returns an error if user action is required to
// configure the firewall rules, or nil if the firewall rules are set up
// properly.
func CheckProjectFirewalls(ctx context.Context, netConfig *InstanceNetworkConfig, instanceProject string) error {
	var err error
	var gceService *compute.Service
	if gceService, err = newGCEService(ctx); err != nil {
		return fmt.Errorf("Failed to start GCE service for setup: %+v", err)
	}

	networkUrl := ProjectNetworkUrls(netConfig, instanceProject)
	projects := []string{*netConfig.NetworkProject, *netConfig.SubnetProject}
	for i, url := range []string{networkUrl} {
		if url == "" {
			continue
		}
		log.Printf("Checking WinRM firewall rule is present for project %s, network %s", projects[i], url)
		if !winRMIngressIsAllowed(gceService, projects[i], url) {
			return fmt.Errorf("Project %s does not have a firewall rule to allow WinRM ingress. Please run:\n  gcloud compute firewall-rules create --project=%s allow-winrm-ingress --allow=tcp:5986 --direction=INGRESS --network=%s", projects[i], projects[i], url)
		}
	}
	return nil
}

// Returns true if the network referenced by networkUrl has a firewall rule
// configured that allows ingress from all source IP addresses on tcp:5986.
func winRMIngressIsAllowed(service *compute.Service, networkProject string, networkUrl string) bool {
	firewalls, err := service.Firewalls.List(networkProject).Do()
	if err != nil {
		log.Printf("firewall list failed: %+v", err)
		return false
	}
	for _, rule := range firewalls.Items {
		for _, allowed := range rule.Allowed {
			if rule.Network == networkUrl && rule.Direction == "INGRESS" && allowed.IPProtocol == "tcp" && rule.SourceRanges[0] == "0.0.0.0/0" && !rule.Disabled {
				for _, port := range allowed.Ports {
					if port == "5986" {
						log.Printf("found an INGRESS firewall rule for tcp:5986 in project %s", networkProject)
						return true
					}
				}
			}
		}
	}
	return false
}

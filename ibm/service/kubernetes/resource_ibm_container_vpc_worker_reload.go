// Copyright IBM Corp. 2017, 2021 All Rights Reserved.
// Licensed under the Mozilla Public License v2.0

package kubernetes

import (
	"fmt"
	"log"
	"time"

	"github.com/IBM-Cloud/bluemix-go/api/container/containerv1"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

	v2 "github.com/IBM-Cloud/bluemix-go/api/container/containerv2"
	"github.com/IBM-Cloud/terraform-provider-ibm/ibm/conns"
	"github.com/IBM-Cloud/terraform-provider-ibm/ibm/validate"
)

// Worker reload lifecycle states.
const (
	containerWorkerReloadStatusUndeploying = "undeploying"
	containerWorkerReloadStatusUndeployed  = "undeployed"
	containerWorkerReloadStatusPending     = "reload_pending"
	containerWorkerReloadStatusReloading   = "reloading"
	containerWorkerReloadStatusReloaded    = "reloaded"
	containerWorkerReloadStatusDeploying   = "deploying"
	containerWorkerReloadStatusDeployed    = "deployed"
	containerWorkerReloadStatusReloadFail  = "reloading_failed"
	containerWorkerReloadStatusDeployFail  = "deploy_failed"
)

func ResourceIBMContainerVpcWorkerReload() *schema.Resource {

	return &schema.Resource{
		Create:   resourceIBMContainerVpcWorkerReloadCreate,
		Read:     resourceIBMContainerVpcWorkerReloadRead,
		Delete:   resourceIBMContainerVpcWorkerReloadDelete,

		Importer: &schema.ResourceImporter{},
		Timeouts: &schema.ResourceTimeout{
			Create: schema.DefaultTimeout(30 * time.Minute),
			Delete: schema.DefaultTimeout(30 * time.Minute),
		},

		Schema: map[string]*schema.Schema{
			"cluster_name": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "Cluster name",
			},
			"bare_metal_server": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "Bare metal server identifier",
			},
		},
	}
}

func ResourceIBMContainerVPCWorkerReloadValidator() *validate.ResourceValidator {
	validateSchema := make([]validate.ValidateSchema, 0)

	containerVPCWorkerReloadValidator := validate.ResourceValidator{ResourceName: "ibm_container_vpc_worker_reload", Schema: validateSchema}
	return &containerVPCWorkerReloadValidator
}

// Since Worker is being managed by Worker Pool, we can't create new workers
// but we can update/reload the existing workers to the new workers.
func resourceIBMContainerVpcWorkerReloadCreate(d *schema.ResourceData, meta interface{}) error {

	bareMetalServerId := d.Get("bare_metal_server").(string)
	clusterNameorID := d.Get("cluster_name").(string)

	wkClient, err := meta.(conns.ClientSession).VpcContainerAPI()
	if err != nil {
		return err
	}

	clusterClient, err := meta.(conns.ClientSession).ContainerAPI()
	if err != nil {
		return fmt.Errorf("[ERROR] Error initializing container API client: %s", err)
	}

	params := containerv1.WorkerUpdateParam{
		Action: "reload",
	}

	err = clusterClient.Workers().Update(clusterNameorID, bareMetalServerId, params, containerv1.ClusterTargetHeader{})
	if err != nil {
		return fmt.Errorf("[ERROR] Error reloading the worker node from the cluster: %s", err)
	}

	_, err = waitForVpcWorkerReloadAvailable(wkClient.Workers(), clusterNameorID, bareMetalServerId, d.Timeout(schema.TimeoutCreate))
	if err != nil {
		return err
	}

	d.SetId(bareMetalServerId)

	return resourceIBMContainerVpcWorkerReloadRead(d, meta)
}

func resourceIBMContainerVpcWorkerReloadRead(d *schema.ResourceData, meta interface{}) error {
	//Not importing this resource.
	return nil
}

func resourceIBMContainerVpcWorkerReloadDelete(d *schema.ResourceData, meta interface{}) error {
	// Delete operation clears only the entries from the statefiles as
	// the reload operation involves both deletion & creation of the
	// resource
	d.SetId("")
	return nil
}

func waitForVpcWorkerReloadAvailable(client v2.Workers, clusterID, id string, timeout time.Duration) (interface{}, error) {
	log.Printf("Waiting for worker (%s) to complete reload.", id)
	stateConf := &resource.StateChangeConf{
		Pending: []string{
			containerWorkerReloadStatusUndeploying,
			containerWorkerReloadStatusUndeployed,
			containerWorkerReloadStatusPending,
			containerWorkerReloadStatusReloading,
			containerWorkerReloadStatusReloaded,
			containerWorkerReloadStatusDeploying,
		},
		Target: []string{
			containerWorkerReloadStatusDeployed,
			containerWorkerReloadStatusDeployFail,
			containerWorkerReloadStatusReloadFail,
		},
		Refresh:    vpcWorkerReloadRefreshFunc(client, clusterID, id),
		Timeout:    timeout,
		Delay:      20 * time.Second,
		MinTimeout: 10 * time.Second,
	}
	return stateConf.WaitForState()
}

func vpcWorkerReloadRefreshFunc(client v2.Workers, clusterID, workerID string) resource.StateRefreshFunc {
	return func() (interface{}, string, error) {
		worker, err := client.Get(clusterID, workerID, v2.ClusterTargetHeader{})
		if err != nil {
			return nil, "", fmt.Errorf("[ERROR] Error getting container vpc worker node: %s", err)
		}

		switch worker.LifeCycle.ActualState {
		case containerWorkerReloadStatusReloadFail:
			return worker, worker.LifeCycle.ActualState, fmt.Errorf("[ERROR] Worker node is in reloading_failed state")
		case containerWorkerReloadStatusDeployFail:
			return worker, worker.LifeCycle.ActualState, fmt.Errorf("[ERROR] Worker node is in deploy_failed state")
		}

		return worker, worker.LifeCycle.ActualState, nil
	}
}

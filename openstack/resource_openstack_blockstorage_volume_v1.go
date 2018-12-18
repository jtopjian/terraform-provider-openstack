package openstack

import (
	"fmt"
	"log"
	"time"

	"github.com/gophercloud/gophercloud/openstack/blockstorage/v1/volumes"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/extensions/volumeattach"

	"github.com/hashicorp/terraform/helper/resource"
	"github.com/hashicorp/terraform/helper/schema"
)

func resourceBlockStorageVolumeV1() *schema.Resource {
	return &schema.Resource{
		Create: resourceBlockStorageVolumeV1Create,
		Read:   resourceBlockStorageVolumeV1Read,
		Update: resourceBlockStorageVolumeV1Update,
		Delete: resourceBlockStorageVolumeV1Delete,
		Importer: &schema.ResourceImporter{
			State: schema.ImportStatePassthrough,
		},

		Timeouts: &schema.ResourceTimeout{
			Create: schema.DefaultTimeout(10 * time.Minute),
			Delete: schema.DefaultTimeout(10 * time.Minute),
		},

		Schema: map[string]*schema.Schema{
			"region": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
				Computed: true,
				ForceNew: true,
			},

			"size": &schema.Schema{
				Type:     schema.TypeInt,
				Required: true,
				ForceNew: true,
			},

			"name": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
				ForceNew: false,
			},

			"description": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
				ForceNew: false,
			},

			"availability_zone": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
				ForceNew: true,
				Computed: true,
			},

			"metadata": &schema.Schema{
				Type:     schema.TypeMap,
				Optional: true,
				ForceNew: false,
				Computed: true,
			},

			"snapshot_id": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
				ForceNew: true,
			},

			"source_vol_id": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
				ForceNew: true,
			},

			"image_id": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
				ForceNew: true,
			},

			"volume_type": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
				ForceNew: true,
				Computed: true,
			},

			"attachment": &schema.Schema{
				Type:     schema.TypeSet,
				Computed: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"id": &schema.Schema{
							Type:     schema.TypeString,
							Computed: true,
						},
						"instance_id": &schema.Schema{
							Type:     schema.TypeString,
							Computed: true,
						},
						"device": &schema.Schema{
							Type:     schema.TypeString,
							Computed: true,
						},
					},
				},
				Set: blockStorageVolumeV1AttachmentHash,
			},
		},
	}
}

func resourceBlockStorageVolumeV1Create(d *schema.ResourceData, meta interface{}) error {
	config := meta.(*Config)
	blockStorageClient, err := config.blockStorageV1Client(GetRegion(d, config))
	if err != nil {
		return fmt.Errorf("Error creating OpenStack block storage client: %s", err)
	}

	metadata := d.Get("metadata").(map[string]interface{})
	createOpts := &volumes.CreateOpts{
		Description:      d.Get("description").(string),
		AvailabilityZone: d.Get("availability_zone").(string),
		Name:             d.Get("name").(string),
		Size:             d.Get("size").(int),
		SnapshotID:       d.Get("snapshot_id").(string),
		SourceVolID:      d.Get("source_vol_id").(string),
		ImageID:          d.Get("image_id").(string),
		VolumeType:       d.Get("volume_type").(string),
		Metadata:         expandBlockStorageVolumeV1Metadata(metadata),
	}

	log.Printf("[DEBUG] openstack_blockstorage_volume_v1 create options: %#v", createOpts)

	v, err := volumes.Create(blockStorageClient, createOpts).Extract()
	if err != nil {
		return fmt.Errorf("Error creating openstack_blockstorage_volume_v1: %s", err)
	}

	stateConf := &resource.StateChangeConf{
		Pending:    []string{"downloading", "creating"},
		Target:     []string{"available"},
		Refresh:    blockStorageVolumeV1StateRefreshFunc(blockStorageClient, v.ID),
		Timeout:    d.Timeout(schema.TimeoutCreate),
		Delay:      10 * time.Second,
		MinTimeout: 3 * time.Second,
	}

	_, err = stateConf.WaitForState()
	if err != nil {
		return fmt.Errorf(
			"Error waiting for openstack_blockstorage_volume_v1 %s to become ready: %s", v.ID, err)
	}

	// Store the ID now
	d.SetId(v.ID)

	return resourceBlockStorageVolumeV1Read(d, meta)
}

func resourceBlockStorageVolumeV1Read(d *schema.ResourceData, meta interface{}) error {
	config := meta.(*Config)

	blockStorageClient, err := config.blockStorageV1Client(GetRegion(d, config))
	if err != nil {
		return fmt.Errorf("Error creating OpenStack block storage client: %s", err)
	}

	v, err := volumes.Get(blockStorageClient, d.Id()).Extract()
	if err != nil {
		return CheckDeleted(d, err, "Error retrieving openstack_blockstorage_volume_v1")
	}

	log.Printf("[DEBUG] Retrieved openstack_blockstorage_volume_v1 %s: %#v", d.Id(), v)

	d.Set("size", v.Size)
	d.Set("description", v.Description)
	d.Set("availability_zone", v.AvailabilityZone)
	d.Set("name", v.Name)
	d.Set("snapshot_id", v.SnapshotID)
	d.Set("source_vol_id", v.SourceVolID)
	d.Set("volume_type", v.VolumeType)
	d.Set("metadata", v.Metadata)
	d.Set("region", GetRegion(d, config))

	attachments := flattenBlockStorageVolumeV1Attachments(v.Attachments)
	log.Printf("[DEBUG] openstack_blockstorage_volume_v1 %s attachments: %#v", d.Id(), attachments)
	if err := d.Set("attachment", attachments); err != nil {
		log.Printf(
			"[DEBUG] unable to set openstack_blockstorage_volume_v1 %s attachments: %s", d.Id(), err)
	}

	return nil
}

func resourceBlockStorageVolumeV1Update(d *schema.ResourceData, meta interface{}) error {
	config := meta.(*Config)
	blockStorageClient, err := config.blockStorageV1Client(GetRegion(d, config))
	if err != nil {
		return fmt.Errorf("Error creating OpenStack block storage client: %s", err)
	}

	name := d.Get("name").(string)
	description := d.Get("description").(string)
	updateOpts := volumes.UpdateOpts{
		Name:        &name,
		Description: &description,
	}

	if d.HasChange("metadata") {
		metadata := d.Get("metadata").(map[string]interface{})
		updateOpts.Metadata = expandBlockStorageVolumeV1Metadata(metadata)
	}

	_, err = volumes.Update(blockStorageClient, d.Id(), updateOpts).Extract()
	if err != nil {
		return fmt.Errorf("Error updating openstack_blockstorage_volume_v1 %s: %s", d.Id(), err)
	}

	return resourceBlockStorageVolumeV1Read(d, meta)
}

func resourceBlockStorageVolumeV1Delete(d *schema.ResourceData, meta interface{}) error {
	config := meta.(*Config)
	blockStorageClient, err := config.blockStorageV1Client(GetRegion(d, config))
	if err != nil {
		return fmt.Errorf("Error creating OpenStack block storage client: %s", err)
	}

	v, err := volumes.Get(blockStorageClient, d.Id()).Extract()
	if err != nil {
		return CheckDeleted(d, err, "Error retrieving openstack_blockstorage_volume_v1")
	}

	// Make sure this volume is detached from all instances before deleting.
	if len(v.Attachments) > 0 {
		computeClient, err := config.computeV2Client(GetRegion(d, config))
		if err != nil {
			return fmt.Errorf("Error creating OpenStack compute client: %s", err)
		}

		for _, volumeAttachment := range v.Attachments {
			log.Printf("[DEBUG] openstack_blockstorage_volume_v1 %s attachment: %#v", d.Id(), volumeAttachment)

			serverID := volumeAttachment["server_id"].(string)
			attachmentID := volumeAttachment["id"].(string)
			if err := volumeattach.Delete(computeClient, serverID, attachmentID).ExtractErr(); err != nil {
				return fmt.Errorf(
					"Error detaching openstack_blockstorage_volume_v1 %s from %s: %s", d.Id(), serverID, err)
			}
		}

		stateConf := &resource.StateChangeConf{
			Pending:    []string{"in-use", "attaching", "detaching"},
			Target:     []string{"available"},
			Refresh:    blockStorageVolumeV1StateRefreshFunc(blockStorageClient, d.Id()),
			Timeout:    10 * time.Minute,
			Delay:      10 * time.Second,
			MinTimeout: 3 * time.Second,
		}

		_, err = stateConf.WaitForState()
		if err != nil {
			return fmt.Errorf(
				"Error waiting for openstack_blockstorage_volume_v1 %s to become available: %s", d.Id(), err)
		}
	}

	// It's possible that this volume was used as a boot device and is currently
	// in a "deleting" state from when the instance was terminated.
	// If this is true, just move on. It'll eventually delete.
	if v.Status != "deleting" {
		if err := volumes.Delete(blockStorageClient, d.Id()).ExtractErr(); err != nil {
			return CheckDeleted(d, err, "Error deleting openstack_blockstorage_volume_v1")
		}
	}

	stateConf := &resource.StateChangeConf{
		Pending:    []string{"deleting", "downloading", "available"},
		Target:     []string{"deleted"},
		Refresh:    blockStorageVolumeV1StateRefreshFunc(blockStorageClient, d.Id()),
		Timeout:    d.Timeout(schema.TimeoutDelete),
		Delay:      10 * time.Second,
		MinTimeout: 3 * time.Second,
	}

	_, err = stateConf.WaitForState()
	if err != nil {
		return fmt.Errorf("Error waiting for openstack_blockstorage_volume_v1 %s to delete: %s", d.Id(), err)
	}

	return nil
}

package openstack

import (
	"fmt"
	"log"
	"time"

	"github.com/gophercloud/gophercloud/openstack/dns/v2/recordsets"

	"github.com/hashicorp/terraform/helper/resource"
	"github.com/hashicorp/terraform/helper/schema"
)

func resourceDNSRecordSetV2() *schema.Resource {
	return &schema.Resource{
		Create: resourceDNSRecordSetV2Create,
		Read:   resourceDNSRecordSetV2Read,
		Update: resourceDNSRecordSetV2Update,
		Delete: resourceDNSRecordSetV2Delete,
		Importer: &schema.ResourceImporter{
			State: schema.ImportStatePassthrough,
		},

		Timeouts: &schema.ResourceTimeout{
			Create: schema.DefaultTimeout(10 * time.Minute),
			Update: schema.DefaultTimeout(10 * time.Minute),
			Delete: schema.DefaultTimeout(10 * time.Minute),
		},

		Schema: map[string]*schema.Schema{
			"region": {
				Type:     schema.TypeString,
				Optional: true,
				ForceNew: true,
				Computed: true,
			},

			"zone_id": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},

			"name": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},

			"description": {
				Type:     schema.TypeString,
				Optional: true,
				ForceNew: false,
			},

			"records": {
				Type:             schema.TypeList,
				Optional:         true,
				ForceNew:         false,
				Elem:             &schema.Schema{Type: schema.TypeString},
				DiffSuppressFunc: dnsRecordSetV2SuppressRecordDiffs,
			},

			"ttl": {
				Type:     schema.TypeInt,
				Optional: true,
				Computed: true,
				ForceNew: false,
			},

			"type": {
				Type:     schema.TypeString,
				Optional: true,
				Computed: true,
				ForceNew: true,
			},

			"value_specs": {
				Type:     schema.TypeMap,
				Optional: true,
				ForceNew: true,
			},
		},
	}
}

func resourceDNSRecordSetV2Create(d *schema.ResourceData, meta interface{}) error {
	config := meta.(*Config)
	dnsClient, err := config.dnsV2Client(GetRegion(d, config))
	if err != nil {
		return fmt.Errorf("Error creating OpenStack DNS client: %s", err)
	}

	records := expandDNSRecordSetV2Records(d.Get("records").([]interface{}))

	createOpts := RecordSetCreateOpts{
		recordsets.CreateOpts{
			Name:        d.Get("name").(string),
			Description: d.Get("description").(string),
			Records:     records,
			TTL:         d.Get("ttl").(int),
			Type:        d.Get("type").(string),
		},
		MapValueSpecs(d),
	}

	log.Printf("[DEBUG] openstack_dns_recordset_v2 create options: %#v", createOpts)

	zoneID := d.Get("zone_id").(string)
	n, err := recordsets.Create(dnsClient, zoneID, createOpts).Extract()
	if err != nil {
		return fmt.Errorf("Error creating openstack_dns_recordset_v2: %s", err)
	}

	stateConf := &resource.StateChangeConf{
		Target:     []string{"ACTIVE"},
		Pending:    []string{"PENDING"},
		Refresh:    dnsRecordSetV2RefreshFunc(dnsClient, zoneID, n.ID),
		Timeout:    d.Timeout(schema.TimeoutCreate),
		Delay:      5 * time.Second,
		MinTimeout: 3 * time.Second,
	}

	_, err = stateConf.WaitForState()

	id := fmt.Sprintf("%s/%s", zoneID, n.ID)
	d.SetId(id)

	log.Printf("[DEBUG] Created openstack_dns_recordset_v2 %s: %#v", n.ID, n)
	return resourceDNSRecordSetV2Read(d, meta)
}

func resourceDNSRecordSetV2Read(d *schema.ResourceData, meta interface{}) error {
	config := meta.(*Config)
	dnsClient, err := config.dnsV2Client(GetRegion(d, config))
	if err != nil {
		return fmt.Errorf("Error creating OpenStack DNS client: %s", err)
	}

	// Obtain relevant info from parsing the ID
	zoneID, recordsetID, err := dnsRecordSetV2ParseID(d.Id())
	if err != nil {
		return err
	}

	n, err := recordsets.Get(dnsClient, zoneID, recordsetID).Extract()
	if err != nil {
		msg := fmt.Sprintf("Error retrieving openstack_dns_recordset_v2 %s", d.Id())
		return CheckDeleted(d, err, msg)
	}

	log.Printf("[DEBUG] Retrieved openstack_dns_recordset_v2 %s: %#v", recordsetID, n)

	d.Set("name", n.Name)
	d.Set("description", n.Description)
	d.Set("ttl", n.TTL)
	d.Set("type", n.Type)
	d.Set("records", n.Records)
	d.Set("zone_id", zoneID)
	d.Set("region", GetRegion(d, config))

	return nil
}

func resourceDNSRecordSetV2Update(d *schema.ResourceData, meta interface{}) error {
	config := meta.(*Config)
	dnsClient, err := config.dnsV2Client(GetRegion(d, config))
	if err != nil {
		return fmt.Errorf("Error creating OpenStack DNS client: %s", err)
	}

	var updateOpts recordsets.UpdateOpts
	if d.HasChange("ttl") {
		updateOpts.TTL = d.Get("ttl").(int)
	}

	if d.HasChange("records") {
		records := expandDNSRecordSetV2Records(d.Get("records").([]interface{}))
		updateOpts.Records = records
	}

	if d.HasChange("description") {
		description := d.Get("description").(string)
		updateOpts.Description = &description
	}

	// Obtain relevant info from parsing the ID
	zoneID, recordsetID, err := dnsRecordSetV2ParseID(d.Id())
	if err != nil {
		return err
	}

	log.Printf("[DEBUG] Updating openstack_dns_recordset_v2 %s with options: %#v", recordsetID, updateOpts)

	_, err = recordsets.Update(dnsClient, zoneID, recordsetID, updateOpts).Extract()
	if err != nil {
		return fmt.Errorf("Error updating openstack_dns_recordset_v2 %s: %s", d.Id(), err)
	}

	stateConf := &resource.StateChangeConf{
		Target:     []string{"ACTIVE"},
		Pending:    []string{"PENDING"},
		Refresh:    dnsRecordSetV2RefreshFunc(dnsClient, zoneID, recordsetID),
		Timeout:    d.Timeout(schema.TimeoutUpdate),
		Delay:      5 * time.Second,
		MinTimeout: 3 * time.Second,
	}

	_, err = stateConf.WaitForState()

	return resourceDNSRecordSetV2Read(d, meta)
}

func resourceDNSRecordSetV2Delete(d *schema.ResourceData, meta interface{}) error {
	config := meta.(*Config)
	dnsClient, err := config.dnsV2Client(GetRegion(d, config))
	if err != nil {
		return fmt.Errorf("Error creating OpenStack DNS client: %s", err)
	}

	// Obtain relevant info from parsing the ID
	zoneID, recordsetID, err := dnsRecordSetV2ParseID(d.Id())
	if err != nil {
		return err
	}

	err = recordsets.Delete(dnsClient, zoneID, recordsetID).ExtractErr()
	if err != nil {
		msg := fmt.Sprintf("Error deleting openstack_dns_recordset_v2 %s", d.Id())
		return CheckDeleted(d, err, msg)
	}

	stateConf := &resource.StateChangeConf{
		Target:     []string{"DELETED"},
		Pending:    []string{"ACTIVE", "PENDING"},
		Refresh:    dnsRecordSetV2RefreshFunc(dnsClient, zoneID, recordsetID),
		Timeout:    d.Timeout(schema.TimeoutDelete),
		Delay:      5 * time.Second,
		MinTimeout: 3 * time.Second,
	}

	_, err = stateConf.WaitForState()

	return nil
}

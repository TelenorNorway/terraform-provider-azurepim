package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccGroupEligibleAssignmentResource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		ExternalProviders: map[string]resource.ExternalProvider{
			"azuread": {
				Source:            "hashicorp/azuread",
				VersionConstraint: "2.47.0",
			},
		},
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: testAccGroupEligibleAssignmentConfig(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("azurepim_group_eligible_assignment.test", "role", "member"),
				),
			},
			// ImportState testing
			{
				ResourceName:      "azurepim_group_eligible_assignment.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// testAccGroupEligibleAssignmentConfig the config requires the following graph permissions in addition to the ones required by the azurepim_group_eligible_assignment resource:
// - RoleManagement.ReadWrite.Directory.
// - Group.Create.
func testAccGroupEligibleAssignmentConfig() string {
	return `
data "azuread_client_config" "current" {}

resource "azuread_group" "main" {
	display_name     = "azurepim-acc-test-group"
	owners           = [data.azuread_client_config.current.object_id]
	security_enabled = true
}

resource "azuread_group" "pag" {
	display_name       = "azurepim-acc-test-group-pag"
	owners             = [data.azuread_client_config.current.object_id]
	security_enabled   = true
}

resource "azurepim_group_eligible_assignment" "test" {
	role          = "member"
	scope         = azuread_group.pag.object_id
	justification = "this is a test"
	principal_id  = azuread_group.main.object_id
}`
}

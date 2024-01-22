terraform {
  required_providers {
    azurepim = {
      source = "telenornorway/azurepim"
    }
  }
}

provider "azurepim" {}

resource "azurepim_group_eligible_assignment" "example" {
  role          = "member"
  scope         = "6313f603-4f44-437b-a074-82d99cd5bed3"
  justification = "because i can"
  principal_id  = "03df64c6-450c-4047-a9bc-1819006f1b51"
}

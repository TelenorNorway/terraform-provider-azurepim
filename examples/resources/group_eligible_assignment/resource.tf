terraform {
  required_providers {
    azurepim = {
      source = "telenornorway/azurepim"
    }
    azuread = {
      source  = "hashicorp/azuread"
      version = "2.48.0"
    }
  }
}

provider "azurepim" {}

data "azuread_client_config" "current" {}

resource "azuread_group" "main" {
  display_name     = "p-grp-1"
  owners           = [data.azuread_client_config.current.object_id]
  security_enabled = true
}

resource "azuread_group" "pag" {
  display_name     = "p-pag-1"
  owners           = [data.azuread_client_config.current.object_id]
  security_enabled = true
}

resource "azurepim_group_eligible_assignment" "main" {
  role          = "member"
  scope         = azuread_group.pag.object_id
  justification = "this is a test"
  principal_id  = azuread_group.main.object_id
}

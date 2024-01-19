terraform {
  required_providers {
    azurepim = {
      source = "akselleirv/azurepim"
    }
    time = {
      source  = "hashicorp/time"
      version = "0.10.0"
    }
  }
}

provider "azurepim" {}

provider "time" {}

resource "time_static" "example" {}

resource "time_offset" "example" {
  offset_days = 7
}

resource "azurepim_group_eligible_assignment" "example" {
  role          = "member"
  scope         = "6313f603-4f44-437b-a074-82d99cd5bed3"
  justification = "because i can"
  principal_id  = "03df64c6-450c-4047-a9bc-1819006f1b51"
  schedule {
    start_date_time = time_static.example.rfc3339
    expiration {
      type          = "test"
      end_date_time = time_offset.example.rfc3339
    }
  }
}

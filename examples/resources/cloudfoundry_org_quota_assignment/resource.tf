terraform {
  required_providers {
    cloudfoundry = {
      source = "cloudfoundry/cloudfoundry"
    }
  }
}
provider "cloudfoundry" {}

resource "cloudfoundry_org" "my_org" {
  name = "my-org"
}

resource "cloudfoundry_org_quota" "my_org_quota" {
  name                     = "my-org-quota"
  allow_paid_service_plans = true
  total_memory             = 1024
}

# On destroy, the org is moved to the quota named "default", or to "fallback_quota" if it is set.
# The assignment is destroyed first, so the quota and the org can be deleted afterwards.
resource "cloudfoundry_org_quota_assignment" "my_org" {
  org   = cloudfoundry_org.my_org.id
  quota = cloudfoundry_org_quota.my_org_quota.id
}

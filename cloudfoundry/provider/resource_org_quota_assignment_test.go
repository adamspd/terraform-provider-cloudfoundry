package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// hclOrgQuotaAssignmentSetup returns the org and the three org quotas used by the test.
//
// The dependencies set the order in which Terraform destroys the resources:
//   - "second" depends on the org, so it is deleted before the org. After the second step of the test the org
//     is assigned to it, so this is the failure of the "orgs" attribute of "cloudfoundry_org_quota":
//     the quota cannot be deleted while it is applied to the org. It only works if the assignment moves the org first.
//   - the org depends on "fallback", so "fallback" is deleted after the org. The org is moved to it on destroy.
//
// The chain also gives the API calls the same order on every run, because they are replayed in the order they were recorded.
const hclOrgQuotaAssignmentSetup = `
resource "cloudfoundry_org_quota" "first" {
  name                     = "tf-unit-test-assign-first"
  allow_paid_service_plans = true
  total_memory             = 1024
}

resource "cloudfoundry_org_quota" "fallback" {
  name                     = "tf-unit-test-assign-fallback"
  allow_paid_service_plans = true
  total_memory             = 512
  depends_on               = [cloudfoundry_org_quota.first]
}

resource "cloudfoundry_org" "rs" {
  name       = "tf-unit-test-assign-org"
  depends_on = [cloudfoundry_org_quota.fallback]
}

resource "cloudfoundry_org_quota" "second" {
  name                     = "tf-unit-test-assign-second"
  allow_paid_service_plans = true
  total_memory             = 2048
  depends_on               = [cloudfoundry_org.rs]
}
`

func TestResourceOrgQuotaAssignment(t *testing.T) {
	t.Parallel()
	resourceName := "cloudfoundry_org_quota_assignment.rs"
	// The final destroy of this test fails with "CF-UnprocessableEntity|10008: This quota is applied to one or more
	// organizations" if the assignment does not move the org off its quota first.
	t.Run("happy path - assign, change and import org quota assignment", func(t *testing.T) {
		cfg := getCFHomeConf()
		rec := cfg.SetupVCR(t, "fixtures/resource_org_quota_assignment")
		defer stopQuietly(rec)
		resource.Test(t, resource.TestCase{
			IsUnitTest:               true,
			ProtoV6ProviderFactories: getProviders(rec.GetDefaultClient()),
			Steps: []resource.TestStep{
				{
					Config: hclProvider(nil) + hclOrgQuotaAssignmentSetup + `
resource "cloudfoundry_org_quota_assignment" "rs" {
  org            = cloudfoundry_org.rs.id
  quota          = cloudfoundry_org_quota.first.id
  fallback_quota = cloudfoundry_org_quota.fallback.id
}
`,
					Check: resource.ComposeAggregateTestCheckFunc(
						resource.TestCheckResourceAttrPair(resourceName, "id", "cloudfoundry_org.rs", "id"),
						resource.TestCheckResourceAttrPair(resourceName, "org", "cloudfoundry_org.rs", "id"),
						resource.TestCheckResourceAttrPair(resourceName, "quota", "cloudfoundry_org_quota.first", "id"),
						resource.TestCheckResourceAttrPair(resourceName, "fallback_quota", "cloudfoundry_org_quota.fallback", "id"),
					),
				},
				{
					Config: hclProvider(nil) + hclOrgQuotaAssignmentSetup + `
resource "cloudfoundry_org_quota_assignment" "rs" {
  org            = cloudfoundry_org.rs.id
  quota          = cloudfoundry_org_quota.second.id
  fallback_quota = cloudfoundry_org_quota.fallback.id
}
`,
					Check: resource.ComposeAggregateTestCheckFunc(
						resource.TestCheckResourceAttrPair(resourceName, "id", "cloudfoundry_org.rs", "id"),
						resource.TestCheckResourceAttrPair(resourceName, "quota", "cloudfoundry_org_quota.second", "id"),
					),
				},
				{
					ResourceName:            resourceName,
					ImportState:             true,
					ImportStateIdFunc:       getIdForImport(resourceName),
					ImportStateVerify:       true,
					ImportStateVerifyIgnore: []string{"fallback_quota"},
				},
			},
		})
	})
}

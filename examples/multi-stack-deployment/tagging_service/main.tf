terraform {
  required_providers {
    varlet = {
      source  = "google/varlet"
      version = "~> 1.0"
    }
  }
}

provider "varlet" {
  endpoint  = "localhost:8080"
  namespace = "tagging_service"
}

# Register the tagging_service namespace.
# Configures a webhook and a 2-minute delay before notifying downstream.
# Restricts access to security_tier_1 and security_tier_2.
resource "varlet_namespace" "self" {
  name                  = "tagging_service"
  run_webhook_url       = "http://ci-cd.internal/trigger/tagging_service"
  webhook_delay_minutes = 2
  allowed_consumers     = ["security_tier_1", "security_tier_2"]
}

# Consume inputs from bootstrap (depends on self namespace existing first)
resource "varlet_input" "org_id" {
  source_namespace = "bootstrap"
  name             = "organization_id"
  depends_on       = [varlet_namespace.self]
}

resource "varlet_input" "deploy_id" {
  source_namespace = "bootstrap"
  name             = "deployment_id"
  depends_on       = [varlet_namespace.self]
}

# Export tagging variables
resource "varlet_output" "tag_keys" {
  name       = "tag_keys"
  value      = ["owner", "env", "billing_id"]
  depends_on = [varlet_namespace.self]
}

resource "varlet_output" "env_tag" {
  name       = "environment_tag_value"
  value      = "production"
  depends_on = [varlet_namespace.self]
}

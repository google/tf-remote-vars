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
  namespace = "security_tier_2"
}

# Register namespace. Restricts access to lockdown_enforcer.
resource "varlet_namespace" "self" {
  name              = "security_tier_2"
  run_webhook_url   = "http://ci-cd.internal/trigger/security_tier_2"
  allowed_consumers = ["lockdown_enforcer"]
}

# Consume inputs from tagging_service
resource "varlet_input" "tag_keys" {
  source_namespace = "tagging_service"
  name             = "tag_keys"
  depends_on       = [varlet_namespace.self]
}

resource "varlet_input" "env_tag" {
  source_namespace = "tagging_service"
  name             = "environment_tag_value"
  depends_on       = [varlet_namespace.self]
}

# Export maps of policy ids for levels 2, 3, 4
resource "varlet_output" "policy_ids" {
  name = "tier_2_policy_ids"
  value = {
    level2 = "policy-strict-tier-2"
    level3 = "policy-harden-tier-3"
    level4 = "policy-lockdown-tier-4"
  }
  depends_on = [varlet_namespace.self]
}

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
  namespace = "security_tier_1"
}

# Register namespace. Restricts access to lockdown_enforcer.
resource "varlet_namespace" "self" {
  name              = "security_tier_1"
  run_webhook_url   = "http://ci-cd.internal/trigger/security_tier_1"
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

# Export security policy
resource "varlet_output" "policy_id" {
  name       = "tier_1_policy_id"
  value      = "policy-standard-tier-1"
  depends_on = [varlet_namespace.self]
}

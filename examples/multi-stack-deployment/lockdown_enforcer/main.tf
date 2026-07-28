terraform {
  required_providers {
    varlet = {
      source  = "google/varlet"
      version = "~> 1.0"
    }
    time = {
      source  = "hashicorp/time"
      version = "0.14.0"
    }
  }
}

provider "varlet" {
  endpoint  = "localhost:8080"
  namespace = "lockdown_enforcer"
}

# Register namespace.
# We set allowed_consumers to ["zone_*"] to allow all regional landing zones to consume.
resource "varlet_namespace" "self" {
  name              = "lockdown_enforcer"
  run_webhook_url   = "http://ci-cd.internal/trigger/lockdown_enforcer"
  allowed_consumers = ["zone_*"]
}

# Consumes inputs from upstream stacks
resource "varlet_input" "deploy_id" {
  source_namespace = "bootstrap"
  name             = "deployment_id"
  depends_on = [varlet_namespace.self, time_sleep.wait_30_seconds]
}

resource "varlet_input" "tier_1_policy" {
  source_namespace = "security_tier_1"
  name             = "tier_1_policy_id"
  depends_on = [varlet_namespace.self, time_sleep.wait_30_seconds]
}

resource "varlet_input" "tier_2_policies" {
  source_namespace = "security_tier_2"
  name             = "tier_2_policy_ids"
  depends_on = [varlet_namespace.self, time_sleep.wait_30_seconds]
}

resource "varlet_input" "org_constraints" {
  source_namespace = "policy_engine"
  name             = "org_policy_constraints"
  depends_on = [varlet_namespace.self, time_sleep.wait_30_seconds]
}

# Export lockdown status object
resource "varlet_output" "status" {
  name = "lockdown_status"
  value = {
    enforced  = true
    timestamp = "2026-07-28T12:00:00Z"
  }
  depends_on = [varlet_namespace.self, time_sleep.wait_30_seconds]
}

resource "time_sleep" "wait_30_seconds" {
  depends_on = [varlet_namespace.self]
  create_duration = "30s"
}

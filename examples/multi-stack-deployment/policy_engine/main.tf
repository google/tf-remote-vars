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
  namespace = "policy_engine"
}

# Register namespace. Restricts access to lockdown_enforcer.
resource "varlet_namespace" "self" {
  name              = "policy_engine"
  run_webhook_url   = "http://ci-cd.internal/trigger/policy_engine"
  allowed_consumers = ["lockdown_enforcer"]
}

# Consume organization_id from bootstrap (public namespace)
resource "varlet_input" "org_id" {
  source_namespace = "bootstrap"
  name             = "organization_id"
  depends_on = [varlet_namespace.self, time_sleep.wait_30_seconds]
}

# Export organization policy constraints
resource "varlet_output" "constraints" {
  name       = "org_policy_constraints"
  value      = ["gcp.restrictServiceUsage", "gcp.disableSerialPortAccess"]
  depends_on = [varlet_namespace.self, time_sleep.wait_30_seconds]
}

resource "time_sleep" "wait_30_seconds" {
  depends_on = [varlet_namespace.self]
  create_duration = "30s"
}

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
  namespace = "zone_ap"
}

# Register the consumer namespace.
resource "varlet_namespace" "self" {
  name            = "zone_ap"
  run_webhook_url = "http://ci-cd.internal/trigger/zone_ap"
}

# Consume lockdown status from lockdown_enforcer
resource "varlet_input" "lockdown" {
  source_namespace = "lockdown_enforcer"
  name             = "lockdown_status"
  depends_on       = [varlet_namespace.self]
}

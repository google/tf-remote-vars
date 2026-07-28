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
  namespace = "zone_us"
}

# Register the consumer namespace. Since it does not export variables,
# we do not need to configure allowed_consumers.
resource "varlet_namespace" "self" {
  name            = "zone_us"
  run_webhook_url = "http://ci-cd.internal/trigger/zone_us"
}

# Consume lockdown status from lockdown_enforcer
resource "varlet_input" "lockdown" {
  source_namespace = "lockdown_enforcer"
  name             = "lockdown_status"
  depends_on = [varlet_namespace.self, time_sleep.wait_30_seconds]
}

resource "time_sleep" "wait_30_seconds" {
  depends_on = [varlet_namespace.self]
  create_duration = "30s"
}

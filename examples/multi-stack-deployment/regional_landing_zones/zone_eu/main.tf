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
    null = {
      source  = "hashicorp/null"
      version = "3.3.0"
    }
  }
}

variable "namespace" {
  type        = string
  description = "The namespace for this stack"
  default     = "zone_eu"
}

provider "varlet" {
  endpoint  = "localhost:8080"
  namespace = var.namespace
}

# Register the consumer namespace.
resource "varlet_namespace" "self" {
  name            = var.namespace
  run_webhook_url = "http://ci-cd.internal/trigger/zone_eu"
}

data "varlet_namespace" "lockdown_enforcer" {
  name = "lockdown_enforcer"
}

# Consume lockdown status from lockdown_enforcer
resource "varlet_input" "lockdown" {
  source_namespace = data.varlet_namespace.lockdown_enforcer.name
  name             = "lockdown_status"
  depends_on       = [varlet_namespace.self]
}

resource "time_sleep" "wait_30_seconds" {
  depends_on = [varlet_namespace.self]
  create_duration = "30s"
  triggers = {
    lockdown = varlet_input.lockdown.trigger
  }
}

resource "null_resource" "force_actuation" {
  triggers = time_sleep.wait_30_seconds.triggers

  depends_on = [time_sleep.wait_30_seconds]

  provisioner "local-exec" {
    command = "echo 'Zone EU actuating due to upstream lockdown change'"
  }
}

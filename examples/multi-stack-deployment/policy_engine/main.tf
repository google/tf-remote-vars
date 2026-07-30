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
  default     = "policy_engine"
}

provider "varlet" {
  endpoint  = "localhost:8080"
  namespace = var.namespace
}

# Register namespace. Restricts access to lockdown_enforcer.
resource "varlet_namespace" "self" {
  name              = var.namespace
  run_webhook_url   = "http://ci-cd.internal/trigger/policy_engine"
  allowed_consumers = ["lockdown_enforcer"]
}

data "varlet_namespace" "bootstrap" {
  name = "bootstrap"
}

# Consume organization_id from bootstrap (public namespace)
resource "varlet_input" "org_id" {
  source_namespace = data.varlet_namespace.bootstrap.name
  name             = "organization_id"
  depends_on       = [varlet_namespace.self]
}

# Export organization policy constraints
resource "varlet_output" "constraints" {
  name       = "org_policy_constraints"
  value      = ["gcp.restrictServiceUsage", "gcp.disableSerialPortAccess", "custom.policy-${var.suffix}"]
  depends_on = [null_resource.force_actuation]
}

resource "time_sleep" "wait_30_seconds" {
  depends_on = [varlet_namespace.self]
  create_duration = "30s"
  triggers = {
    org_id = varlet_input.org_id.trigger
  }
}

resource "null_resource" "force_actuation" {
  triggers = time_sleep.wait_30_seconds.triggers

  depends_on = [time_sleep.wait_30_seconds]

  provisioner "local-exec" {
    command = "echo 'Policy Engine actuating due to upstream bootstrap change'"
  }
}

variable "suffix" {
  type        = string
  description = "Suffix to append to output values to demonstrate change propagation"
  default     = "original"
}

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
  default     = "tagging_service"
}

provider "varlet" {
  endpoint  = "localhost:8080"
  namespace = var.namespace
}

# Register the tagging_service namespace.
# Configures a webhook and a 2-minute delay before notifying downstream.
# Restricts access to security_tier_1 and security_tier_2.
resource "varlet_namespace" "self" {
  name                  = var.namespace
  run_webhook_url       = "http://ci-cd.internal/trigger/tagging_service"
  webhook_delay_minutes = 2
  allowed_consumers     = ["security_tier_1", "security_tier_2"]
}

data "varlet_namespace" "bootstrap" {
  name = "bootstrap"
}

# Consume inputs from bootstrap (depends on self namespace existing first)
resource "varlet_input" "org_id" {
  source_namespace = data.varlet_namespace.bootstrap.name
  name             = "organization_id"
  depends_on       = [varlet_namespace.self]
}

resource "varlet_input" "deploy_id" {
  source_namespace = data.varlet_namespace.bootstrap.name
  name             = "deployment_id"
  depends_on       = [varlet_namespace.self]
}

# Export tagging variables
resource "varlet_output" "tag_keys" {
  name       = "tag_keys"
  value      = ["owner", "env", "billing_id", "suffix-${var.suffix}"]
  depends_on = [null_resource.force_actuation]
}

resource "varlet_output" "env_tag" {
  name       = "environment_tag_value"
  value      = "production-${var.suffix}"
  depends_on = [null_resource.force_actuation]
}

resource "time_sleep" "wait_30_seconds" {
  depends_on = [varlet_namespace.self]
  create_duration = "30s"
  triggers = {
    org_id    = varlet_input.org_id.trigger
    deploy_id = varlet_input.deploy_id.trigger
  }
}

resource "null_resource" "force_actuation" {
  triggers = time_sleep.wait_30_seconds.triggers

  depends_on = [time_sleep.wait_30_seconds]

  provisioner "local-exec" {
    command = "echo 'Tagging service actuating due to upstream bootstrap change'"
  }
}

variable "suffix" {
  type        = string
  description = "Suffix to append to output values to demonstrate change propagation"
  default     = "original"
}

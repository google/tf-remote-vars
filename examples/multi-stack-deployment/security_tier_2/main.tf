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
  default     = "security_tier_2"
}

provider "varlet" {
  endpoint  = "localhost:8080"
  namespace = var.namespace
}

# Register namespace. Restricts access to lockdown_enforcer.
resource "varlet_namespace" "self" {
  name              = var.namespace
  run_webhook_url   = "http://ci-cd.internal/trigger/security_tier_2"
  allowed_consumers = ["lockdown_enforcer"]
}

data "varlet_namespace" "tagging_service" {
  name = "tagging_service"
}

# Consume inputs from tagging_service
resource "varlet_input" "tag_keys" {
  source_namespace = data.varlet_namespace.tagging_service.name
  name             = "tag_keys"
  depends_on       = [varlet_namespace.self]
}

resource "varlet_input" "env_tag" {
  source_namespace = data.varlet_namespace.tagging_service.name
  name             = "environment_tag_value"
  depends_on       = [varlet_namespace.self]
}

# Export maps of policy ids for levels 2, 3, 4
resource "varlet_output" "policy_ids" {
  name = "tier_2_policy_ids"
  value = {
    level2 = "policy-strict-tier-2-${var.suffix}"
    level3 = "policy-harden-tier-3-${var.suffix}"
    level4 = "policy-lockdown-tier-4-${var.suffix}"
  }
  depends_on = [null_resource.force_actuation]
}

resource "time_sleep" "wait_30_seconds" {
  depends_on = [varlet_namespace.self]
  create_duration = "30s"
  triggers = {
    tag_keys = varlet_input.tag_keys.trigger
    env_tag  = varlet_input.env_tag.trigger
  }
}

resource "null_resource" "force_actuation" {
  triggers = time_sleep.wait_30_seconds.triggers

  depends_on = [time_sleep.wait_30_seconds]

  provisioner "local-exec" {
    command = "echo 'Security Tier 2 actuating due to upstream tagging change'"
  }
}

variable "suffix" {
  type        = string
  description = "Suffix to append to output values to demonstrate change propagation"
  default     = "original"
}

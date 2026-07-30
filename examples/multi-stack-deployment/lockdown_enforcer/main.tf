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
  default     = "lockdown_enforcer"
}

provider "varlet" {
  endpoint  = "localhost:8080"
  namespace = var.namespace
}

# Register namespace.
# We set allowed_consumers to ["zone_*"] to allow all regional landing zones to consume.
resource "varlet_namespace" "self" {
  name              = var.namespace
  run_webhook_url   = "http://ci-cd.internal/trigger/lockdown_enforcer"
  allowed_consumers = ["zone_*"]
}

data "varlet_namespace" "bootstrap" {
  name = "bootstrap"
}

data "varlet_namespace" "security_tier_1" {
  name = "security_tier_1"
}

data "varlet_namespace" "security_tier_2" {
  name = "security_tier_2"
}

data "varlet_namespace" "policy_engine" {
  name = "policy_engine"
}

# Consumes inputs from upstream stacks
resource "varlet_input" "deploy_id" {
  source_namespace = data.varlet_namespace.bootstrap.name
  name             = "deployment_id"
  depends_on       = [varlet_namespace.self]
}

resource "varlet_input" "tier_1_policy" {
  source_namespace = data.varlet_namespace.security_tier_1.name
  name             = "tier_1_policy_id"
  depends_on       = [varlet_namespace.self]
}

resource "varlet_input" "tier_2_policies" {
  source_namespace = data.varlet_namespace.security_tier_2.name
  name             = "tier_2_policy_ids"
  depends_on       = [varlet_namespace.self]
}

resource "varlet_input" "org_constraints" {
  source_namespace = data.varlet_namespace.policy_engine.name
  name             = "org_policy_constraints"
  depends_on       = [varlet_namespace.self]
}

# Export lockdown status object
resource "varlet_output" "status" {
  name = "lockdown_status"
  value = {
    enforced  = true
    timestamp = timestamp()
  }
  depends_on = [null_resource.force_actuation]
}

resource "time_sleep" "wait_30_seconds" {
  depends_on = [varlet_namespace.self]
  create_duration = "30s"
  triggers = {
    deploy_id       = varlet_input.deploy_id.trigger
    tier_1_policy   = varlet_input.tier_1_policy.trigger
    tier_2_policies = varlet_input.tier_2_policies.trigger
    org_constraints = varlet_input.org_constraints.trigger
  }
}

resource "null_resource" "force_actuation" {
  triggers = time_sleep.wait_30_seconds.triggers

  depends_on = [time_sleep.wait_30_seconds]

  provisioner "local-exec" {
    command = "echo 'Lockdown Enforcer actuating due to upstream changes'"
  }
}

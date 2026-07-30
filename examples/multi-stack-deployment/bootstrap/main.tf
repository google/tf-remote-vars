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

variable "namespace" {
  type        = string
  description = "The namespace for this stack"
  default     = "bootstrap"
}

provider "varlet" {
  endpoint  = "localhost:8080"
  namespace = var.namespace
}

# Register the bootstrap namespace.
# We set allowed_consumers to ["*"] to show how a public namespace can be consumed by anyone.
resource "varlet_namespace" "self" {
  name              = var.namespace
  allowed_consumers = ["*"]
}

# Export organization_id. Must depend on namespace registration.
resource "varlet_output" "organization_id" {
  name       = "organization_id"
  value      = "org-${var.suffix}"
  depends_on = [varlet_namespace.self, time_sleep.wait_30_seconds]
}

# Export deployment_id. Must depend on namespace registration.
resource "varlet_output" "deployment_id" {
  name       = "deployment_id"
  value      = "prod-${var.suffix}"
  depends_on = [varlet_namespace.self, time_sleep.wait_30_seconds]
}

resource "time_sleep" "wait_30_seconds" {
  depends_on = [varlet_namespace.self]
  create_duration = "30s"
}

variable "suffix" {
  type        = string
  description = "Suffix to append to output values to demonstrate change propagation"
  default     = "original"
}

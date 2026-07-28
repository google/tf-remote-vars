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
  namespace = "bootstrap"
}

# Register the bootstrap namespace.
# We set allowed_consumers to ["*"] to show how a public namespace can be consumed by anyone.
resource "varlet_namespace" "self" {
  name              = "bootstrap"
  allowed_consumers = ["*"]
}

# Export organization_id. Must depend on namespace registration.
resource "varlet_output" "organization_id" {
  name       = "organization_id"
  value      = "org-998877"
  depends_on = [varlet_namespace.self, time_sleep.wait_30_seconds]
}

# Export deployment_id. Must depend on namespace registration.
resource "varlet_output" "deployment_id" {
  name       = "deployment_id"
  value      = "deploy-xyz-production"
  depends_on = [varlet_namespace.self, time_sleep.wait_30_seconds]
}

resource "time_sleep" "wait_30_seconds" {
  depends_on = [varlet_namespace.self]
  create_duration = "30s"
}

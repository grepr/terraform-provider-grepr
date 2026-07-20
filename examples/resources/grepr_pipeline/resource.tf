terraform {
  required_providers {
    grepr = {
      source = "grepr/grepr"
    }
  }
}

provider "grepr" {
  # Configure via environment variables:
  # GREPR_HOST, GREPR_CLIENT_ID, GREPR_CLIENT_SECRET
}

resource "grepr_pipeline" "example" {
  name = "example_pipeline"

  job_graph_json = file("${path.module}/pipeline.json")

  desired_state = "RUNNING"

  tags = {
    environment = "example"
    managed_by  = "terraform"
  }

  wait_for_state = true
  state_timeout  = 300
}

output "pipeline_id" {
  description = "The ID of the created pipeline"
  value       = grepr_pipeline.example.id
}

output "pipeline_version" {
  description = "The current version of the pipeline"
  value       = grepr_pipeline.example.version
}

output "pipeline_state" {
  description = "The current state of the pipeline"
  value       = grepr_pipeline.example.state
}

output "pipeline_health" {
  description = "The health status of the pipeline"
  value       = grepr_pipeline.example.pipeline_health
}

terraform {
  required_providers {
    grepr = { source = "grepr/grepr" }
  }
}

# Set GREPR_HOST, GREPR_CLIENT_ID, and GREPR_CLIENT_SECRET for authentication.
provider "grepr" {}

variable "datadog_api_key" {
  type      = string
  sensitive = true
}

resource "grepr_datadog_integration" "datadog" {
  name    = "terraform_datadog"
  site    = "datadoghq.com"
  api_key = var.datadog_api_key
}

resource "grepr_data_warehouse_integration" "warehouse" {
  name = "terraform_warehouse"
}

resource "grepr_dataset" "logs" {
  name           = "terraform_logs"
  integration_id = grepr_data_warehouse_integration.warehouse.id
}

resource "grepr_pipeline" "logs" {
  name = "terraform_logs"
  job_graph_json = jsonencode({
    vertices = [
      {
        type          = "datadog-log-agent-source"
        name          = "source"
        integrationId = grepr_datadog_integration.datadog.id
      },
      {
        type      = "logs-iceberg-table-sink"
        name      = "sink"
        datasetId = grepr_dataset.logs.id
      }
    ]
    edges = ["source -> sink"]
  })
}

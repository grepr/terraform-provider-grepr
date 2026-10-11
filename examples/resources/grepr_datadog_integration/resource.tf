variable "datadog_api_key" {
  type      = string
  sensitive = true
}

variable "datadog_app_key" {
  type      = string
  sensitive = true
}

resource "grepr_datadog_integration" "main" {
  name    = "production_datadog"
  site    = "datadoghq.com"
  api_key = var.datadog_api_key
  app_key = var.datadog_app_key
}

variable "datadog_legacy_api_key" {
  type      = string
  sensitive = true
}

# Several API keys, all accepted for inbound data; one sends data to Datadog.
# After importing an integration, existing keys can be kept by key_id alone.
resource "grepr_datadog_integration" "multi_key" {
  name = "multi_key_datadog"
  site = "datadoghq.com"

  api_keys = {
    primary = { value = var.datadog_api_key, send_key = true }
    legacy  = { value = var.datadog_legacy_api_key, label = "legacy agents" }
  }
}

variable "sumo_collector_url" {
  type      = string
  sensitive = true
}

resource "grepr_sumo_integration" "main" {
  name     = "production_sumo"
  endpoint = var.sumo_collector_url
}

resource "grepr_data_warehouse_integration" "warehouse" {
  name = "terraform_warehouse"
}

resource "grepr_dataset" "logs" {
  name           = "terraform_logs"
  integration_id = grepr_data_warehouse_integration.warehouse.id
}

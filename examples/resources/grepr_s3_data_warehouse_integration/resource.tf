resource "grepr_s3_data_warehouse_integration" "main" {
  name        = "production_s3_warehouse"
  bucket_name = "example-grepr-data"
  region      = "us-east-1"

  # Changing the bucket replaces the integration and anything built on it.
  lifecycle {
    prevent_destroy = true
  }
}

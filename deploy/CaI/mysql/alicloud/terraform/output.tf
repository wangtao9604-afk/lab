output "private_endpoint" {
  value = alicloud_db_instance.this.connection_string
}

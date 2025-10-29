data "alicloud_kvstore_connections" "redis" {
  ids = [alicloud_kvstore_instance.this.id]
}

data "alicloud_kvstore_instance_classes" "available" {
  zone_id              = var.fallback_zone_id
  engine               = "Redis"
  engine_version       = var.redis_engine_version
  instance_charge_type = local.payment_type
}

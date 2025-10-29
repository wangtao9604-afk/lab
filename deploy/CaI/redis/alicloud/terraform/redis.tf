resource "alicloud_kvstore_instance" "this" {
  db_instance_name = var.instance_name
  instance_type    = "Redis"
  engine_version   = var.redis_engine_version
  instance_class   = var.redis_instance_class
  vswitch_id       = local.vswitch_fallback.id
  zone_id          = local.vswitch_fallback.zone_id
  payment_type     = local.payment_type
  period           = local.payment_type == "PrePaid" ? tostring(var.period) : null
  auto_renew       = local.payment_type == "PrePaid" ? var.auto_renew : null
  vpc_auth_mode    = var.redis_password_free ? "Close" : "Open"

  security_ips = var.security_cidrs

  lifecycle {
    precondition {
      condition     = local.class_supported
      error_message = "Fallback 可用区 ${var.fallback_zone_id}（vswitch ${local.vswitch_fallback.id}）不支持规格 ${var.redis_instance_class} 和引擎版本 ${var.redis_engine_version} 的组合，请更换可用区/交换机或调整实例规格、引擎版本。"
    }
  }
}

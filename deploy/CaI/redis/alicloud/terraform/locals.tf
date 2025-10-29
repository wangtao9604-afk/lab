locals {
  payment_type = upper(var.pay_type) == "PREPAID" ? "PrePaid" : "PostPaid"
  vswitch_fallback = {
    id      = var.vswitch_ids[0]
    zone_id = var.fallback_zone_id
  }

  available_classes = distinct(compact([
    for item in coalesce(data.alicloud_kvstore_instance_classes.available.instance_classes, []) : try(
      item.class_code,
      try(
        item["class_code"],
        try(
          item.class,
          try(item["class"], try(item.instance_class, try(item["instance_class"], item, "")))
        )
      )
    )
  ]))

  class_supported = contains(local.available_classes, var.redis_instance_class)
}

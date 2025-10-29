resource "alicloud_alikafka_instance" "this" {
  name            = var.instance_name
  spec_type       = var.kafka_spec
  deploy_type     = 5
  vpc_id          = var.vpc_id
  service_version = "2.6.2"

  paid_type   = lower(var.pay_type) == "prepaid" ? "PrePaid" : "PostPaid"
  vswitch_ids = var.vswitch_ids
  io_max      = var.kafka_io_max
  disk_size   = var.kafka_disk_size
}

resource "alicloud_alikafka_topic" "topics" {
  for_each      = var.kafka_topics
  instance_id   = alicloud_alikafka_instance.this.id
  topic         = each.key
  partition_num = each.value.partitions
  local_topic   = false
  compact_topic = false
  remark        = each.value.remark
}

resource "alicloud_alikafka_instance_allowed_ip_attachment" "allow_vpc" {
  instance_id  = alicloud_alikafka_instance.this.id
  allowed_type = "vpc"
  port_range   = "9092/9092"
  allowed_ip   = var.vpc_cidr
}

resource "alicloud_alikafka_instance_allowed_ip_attachment" "allow_pod" {
  instance_id  = alicloud_alikafka_instance.this.id
  allowed_type = "vpc"
  port_range   = "9092/9092"
  allowed_ip   = var.pod_cidr
}

resource "alicloud_alikafka_consumer_group" "groups" {
  for_each    = var.kafka_consumer_groups
  instance_id = alicloud_alikafka_instance.this.id
  consumer_id = each.value
}

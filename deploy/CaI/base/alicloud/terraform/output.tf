output "vpc_id" {
  value = alicloud_vpc.this.id
}

output "vpc_cidr" {
  value = var.vpc_cidr
}

output "public_vswitch_ids" {
  value = [alicloud_vswitch.vsw_public.id]
}

output "private_vswitch_ids" {
  value = concat(
    [alicloud_vswitch.vsw_first_private.id],
    var.use_multiaz ? [alicloud_vswitch.vsw_second_private[0].id] : []
  )
}

output "pod_vswitch_ids" {
  value = concat(
    [alicloud_vswitch.vsw_first_pod.id],
    var.use_multiaz ? [alicloud_vswitch.vsw_second_pod[0].id] : []
  )
}

output "pod_vswitch_cidrs" {
  value = local.pod_vswitch_cidrs
}

output "pod_cidr" {
  value       = local.pod_cidr
  description = "Aggregated pod CIDR derived from pod vswitch cidrs; falls back to first pod vswitch cidr if they are not contiguous."
}

output "public_vswitch_id_in_second_az" {
  value = alicloud_vswitch.vsw_alb.id
}

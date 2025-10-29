module "base" {
  source                = "../../base/alicloud/terraform"
  first_available_zone  = var.first_available_zone
  second_available_zone = var.second_available_zone
  business_name         = var.business_name
}

# module "registry" {
#   source          = "../../registry/alicloud/terraform"
#   alicloud_region = var.alicloud_region
#   business_name   = var.business_name
# }

module "kubernetes" {
  source                         = "../../kubernetes/alicloud/terraform"
  cluster_name                   = local.cluster_name
  k8s_version                    = var.k8s_version
  vpc_id                         = module.base.vpc_id
  worker_vswitch_ids             = module.base.private_vswitch_ids
  pod_vswitch_ids                = module.base.pod_vswitch_ids
  core_instance_type             = var.core_instance_type
  consumers_instance_type        = var.consumers_instance_type
  system_instance_type           = var.system_instance_type
  create_new_nat_gateway         = false
  first_az                       = var.first_available_zone
  second_az                      = var.second_available_zone
  public_vswitch_id_in_first_az  = module.base.public_vswitch_ids[0]
  public_vswitch_id_in_second_az = module.base.public_vswitch_id_in_second_az
}

module "mysql" {
  source          = "../../mysql/alicloud/terraform"
  vpc_id          = module.base.vpc_id
  vswitch_ids     = module.base.private_vswitch_ids
  zone_id         = var.first_available_zone
  zone_id_slave_a = var.second_available_zone
  rds_account     = "admin_${var.business_name}"
  rds_password    = var.rds_password
  security_cidrs  = [module.base.vpc_cidr, module.base.pod_cidr]
  db_name         = var.business_name
}

module "kafka" {
  source        = "../../kafka/alicloud/terraform"
  vpc_id        = module.base.vpc_id
  vswitch_ids   = module.base.private_vswitch_ids
  vpc_cidr      = module.base.vpc_cidr
  pod_cidr      = module.base.pod_cidr
  instance_name = "kafka-${var.business_name}"
}

module "redis" {
  source           = "../../redis/alicloud/terraform"
  vpc_id           = module.base.vpc_id
  vswitch_ids      = module.base.private_vswitch_ids
  fallback_zone_id = var.first_available_zone
  security_cidrs   = [module.base.vpc_cidr, module.base.pod_cidr]
  instance_name    = "redis-${var.business_name}"
}

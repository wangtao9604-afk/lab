locals {
  pod_vswitch_cidrs = var.use_multiaz ? [
    var.vsw_first_pod_cidr,
    var.vsw_second_pod_cidr,
    ] : [
    var.vsw_first_pod_cidr,
  ]

  # Pod CIDR follows the dedicated secondary CIDR assigned to the VPC, ensuring
  # it is isolated from the primary VPC address space while remaining consistent
  # with pod vswitch allocation requirements.
  pod_cidr = var.vpc_pod_secondary_cidr
}

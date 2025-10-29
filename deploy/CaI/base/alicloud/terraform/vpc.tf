resource "alicloud_vpc" "this" {
  vpc_name   = "${var.business_name}-vpc"
  cidr_block = var.vpc_cidr
}

resource "alicloud_vpc_ipv4_cidr_block" "pod_secondary" {
  vpc_id               = alicloud_vpc.this.id
  secondary_cidr_block = var.vpc_pod_secondary_cidr
}

resource "alicloud_vswitch" "vsw_public" {
  vpc_id       = alicloud_vpc.this.id
  cidr_block   = var.vsw_public_cidr
  zone_id      = var.first_available_zone
  vswitch_name = "${var.business_name}-public-subnet"
}

resource "alicloud_vswitch" "vsw_first_private" {
  vpc_id       = alicloud_vpc.this.id
  cidr_block   = var.vsw_first_private_cidr
  zone_id      = var.first_available_zone
  vswitch_name = "${var.business_name}-first-private-subnet"
}

resource "alicloud_vswitch" "vsw_second_private" {
  count        = var.use_multiaz ? 1 : 0
  vpc_id       = alicloud_vpc.this.id
  cidr_block   = var.vsw_second_private_cidr
  zone_id      = var.second_available_zone
  vswitch_name = "${var.business_name}-second-private-subnet"
}

resource "alicloud_vswitch" "vsw_first_pod" {
  vpc_id       = alicloud_vpc.this.id
  cidr_block   = var.vsw_first_pod_cidr
  zone_id      = var.first_available_zone
  vswitch_name = "${var.business_name}-first-pod-subnet"
  depends_on   = [alicloud_vpc_ipv4_cidr_block.pod_secondary]
}

resource "alicloud_vswitch" "vsw_second_pod" {
  count        = var.use_multiaz ? 1 : 0
  vpc_id       = alicloud_vpc.this.id
  cidr_block   = var.vsw_second_pod_cidr
  zone_id      = var.second_available_zone
  vswitch_name = "${var.business_name}-second-pod-subnet"
  depends_on   = [alicloud_vpc_ipv4_cidr_block.pod_secondary]
}

// alb专用
resource "alicloud_vswitch" "vsw_alb" {
  vpc_id       = alicloud_vpc.this.id
  cidr_block   = var.vsw_alb_public_cidr
  zone_id      = var.second_available_zone
  vswitch_name = "${var.business_name}-alb-subnet"
}

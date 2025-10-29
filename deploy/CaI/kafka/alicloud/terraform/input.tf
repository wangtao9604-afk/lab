variable "vpc_id" {
  type = string
}

variable "instance_name" {
  type = string
}

variable "vswitch_ids" {
  type = list(string)
  validation {
    condition     = length(var.vswitch_ids) > 0
    error_message = "Provide at least one vswitch ID for Kafka."
  }
}

variable "kafka_spec" {
  type        = string
  default     = "professional"
  description = "Kafka instance edition: normal | professional | professionalForHighRead"
}

variable "kafka_topics" {
  type = map(object({
    partitions = number
    remark     = string
  }))
  default = {
    "qywx-kf-ipang" = {
      partitions = 36
      remark     = "kf fanout"
    }
    "qywx-kf-ipang-dlq" = {
      partitions = 36
      remark     = "kf fanout dlq"
    }
    "qywx-recorder" = {
      partitions = 24
      remark     = "recorder sink"
    }
    "wx_raw_event" = {
      partitions = 1
      remark     = "raw callback"
    }
  }
}

variable "pay_type" {
  type    = string
  default = "PostPaid"
}

variable "kafka_consumer_groups" {
  type = set(string)
  default = [
    "qywx-recorder-group",
    "kf-fetcher-group",
    "qywx-consumer-group"
  ]
}

variable "period" {
  type    = number
  default = 1
}

variable "auto_renew" {
  type    = bool
  default = false
}

variable "vpc_cidr" {
  type = string
}

variable "pod_cidr" {
  type = string
}

variable "kafka_io_max" {
  type        = number
  default     = 20
  description = "Kafka instance IO spec (20/30/60/90/120 etc)"
}

variable "kafka_disk_size" {
  type        = number
  default     = 500
  description = "Kafka disk size in GB (500-96000, step 100)"
}

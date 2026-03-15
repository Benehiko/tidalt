variable "VERSION" {
  default = "3.0.0"
}

variable "OUTPUT_DIR" {
  default = "./dist"
}

group "default" {
  targets = ["debian", "arch", "fedora"]
}

target "debian" {
  context    = "."
  dockerfile = "packaging/debian/Dockerfile"
  args = {
    VERSION = VERSION
  }
  platforms = ["linux/amd64", "linux/arm64"]
  output = [{
    type  = "local"
    dest  = OUTPUT_DIR
  }]
}

target "arch" {
  context    = "."
  dockerfile = "packaging/arch/Dockerfile"
  args = {
    VERSION = VERSION
  }
  platforms = ["linux/amd64"]
  output = [{
    type  = "local"
    dest  = OUTPUT_DIR
  }]
}

target "fedora" {
  context    = "."
  dockerfile = "packaging/fedora/Dockerfile"
  args = {
    VERSION = VERSION
  }
  platforms = ["linux/amd64", "linux/arm64"]
  output = [{
    type  = "local"
    dest  = OUTPUT_DIR
  }]
}

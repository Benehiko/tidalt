variable "VERSION" {
  default = "3.0.0"
}

variable "OUTPUT_DIR" {
  default = "./dist"
}

# DOCKER_TAG is set to the full Git tag (e.g. "v3.0.0") in CI.
variable "DOCKER_TAG" {
  default = "latest"
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

# ── Docker image (pushed to Docker Hub) ──────────────────────────────────────
# Not part of the default group; invoked explicitly in CI via:
#   DOCKER_TAG=v3.x.x docker buildx bake docker --push
target "docker" {
  context    = "."
  dockerfile = "packaging/docker/Dockerfile"
  args = {
    VERSION = VERSION
  }
  platforms = ["linux/amd64", "linux/arm64"]
  tags = [
    "benehiko/tidalt:${DOCKER_TAG}",
    "benehiko/tidalt:latest",
  ]
}

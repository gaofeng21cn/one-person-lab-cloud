#!/usr/bin/env bash
set -euo pipefail

registry="opl-qualification-workspace-${GITHUB_RUN_ID:?}-${GITHUB_RUN_ATTEMPT:?}"
builder="${registry}-buildkit"

push_image() {
  local tag="$1"
  docker push "$tag" >&2
  local digest
  digest="$(docker image inspect --format '{{range .RepoDigests}}{{println .}}{{end}}' "$tag" |
    awk -v prefix="${tag%:*}@" 'index($0, prefix) == 1')"
  [[ "$digest" == "${tag%:*}@sha256:"* ]]
  [[ "${digest##*@}" =~ ^sha256:[a-f0-9]{64}$ ]]
  printf '%s\n' "$digest"
}

case "${1:?}" in
  prepare)
    # The daemon and registry share a network namespace. The runner and local
    # Docker Engine use the same loopback registry address through its port map.
    port="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1])')"
    registry_host="127.0.0.1:$port"
    prefix="$registry_host/qualification-fixtures"
    config="$RUNNER_TEMP/$builder.toml"
    printf '[registry."%s"]\n  http = true\n' "$registry_host" > "$config"
    docker run -d --name "$registry" -p "$registry_host:$port" -e REGISTRY_HTTP_ADDR="0.0.0.0:$port" \
      registry:2@sha256:a3d8aaa63ed8681a604f1dea0aa03f100d5895b6a58ace528858a7b332415373
    printf 'registry=%s\nregistry_host=%s\n' "$registry" "$registry_host" >> "$GITHUB_OUTPUT"
    docker run -d --privileged --name "$builder" --network "container:$registry" \
      --mount "type=bind,src=$config,dst=/etc/buildkit/buildkitd.toml,readonly" \
      moby/buildkit@sha256:c5f8862f52499773af85bc07695b2379ef08e8002352b0df2c4864c91d78dabe \
      --addr tcp://0.0.0.0:1234
    curl --fail --silent --show-error --retry 15 --retry-connrefused "http://$registry_host/v2/"

    docker build --platform linux/amd64 --file deploy/portable/qualification-workspace.Dockerfile \
      --label "org.opencontainers.image.revision=$PRODUCT_SHA" --tag "$prefix/runtime:fixture" .
    runtime="$(push_image "$prefix/runtime:fixture")"
    docker build --platform linux/amd64 --tag "$prefix/webui:fixture" webui/cloud-agent
    webui="$(push_image "$prefix/webui:fixture")"
    frontend_source="docker/dockerfile@sha256:ecfaec9ed6d810b56388c508f4121597bfbba70d41a6dfeee4d8cad5f295fc32"
    frontend_digest="$(docker buildx imagetools inspect --raw "$frontend_source" |
      jq -er '.manifests[] | select(.platform.os=="linux" and .platform.architecture=="amd64") | .digest')"
    [[ "$frontend_digest" =~ ^sha256:[a-f0-9]{64}$ ]]
    docker pull "docker/dockerfile@$frontend_digest"
    docker tag "docker/dockerfile@$frontend_digest" "$prefix/frontend:fixture"
    frontend="$(push_image "$prefix/frontend:fixture")"

    recipe_dir="$RUNNER_TEMP/$registry-recipe"
    mkdir -p "$recipe_dir"
    # Byte-for-byte services/build/internal/build.FixedDockerfile for the
    # clean-host Runtime contract and the repository customer fixture layout.
    cat > "$recipe_dir/Dockerfile" <<EOF
# syntax=$frontend
FROM runtime
COPY --from=agent_package --chown=1000:1000 agent /agent
COPY --from=webui --chown=1000:1000 /web /web
ENTRYPOINT ["/agent/bin/runtime"]
CMD ["serve"]
EOF
    tar -C "$recipe_dir" -cf "$recipe_dir/recipe.tar" Dockerfile
    digest="sha256:$(sha256sum "$recipe_dir/recipe.tar" | cut -d ' ' -f 1)"
    location="$(curl --fail --silent --show-error --dump-header - --output /dev/null -X POST \
      "http://$registry_host/v2/qualification-fixtures/recipe/blobs/uploads/" |
      awk 'tolower($1) == "location:" {sub(/\r$/, "", $2); print $2}')"
    case "$location" in
      /v2/qualification-fixtures/recipe/blobs/uploads/*) location="http://$registry_host$location" ;;
      "http://$registry_host"/v2/qualification-fixtures/recipe/blobs/uploads/*) ;;
      *) printf 'Unexpected recipe upload location\n' >&2; exit 1 ;;
    esac
    curl --fail --silent --show-error --output /dev/null -X PUT \
      -H 'Content-Type: application/octet-stream' --data-binary "@$recipe_dir/recipe.tar" \
      "$location&digest=$digest"
    test "$(curl --fail --silent --show-error "http://$registry_host/v2/qualification-fixtures/recipe/blobs/$digest" |
      sha256sum | cut -d ' ' -f 1)" = "${digest#sha256:}"

    workspace="${SUPPLIED_WORKSPACE_IMAGE:-}"
    if [ -z "$workspace" ]; then
      # Capability rejects overlapping Publisher Namespace prefixes. The
      # standalone Runtime's installation owns its own exact repository.
      standalone="$registry_host/opl-qualification-workspace:fixture"
      docker tag "$prefix/runtime:fixture" "$standalone"
      workspace="$(push_image "$standalone")"
    fi
    [[ "$workspace" =~ ^[a-z0-9.-]+(:[1-9][0-9]*)?/[a-z0-9._/-]+@sha256:[a-f0-9]{64}$ ]]
    {
      printf 'image_ref=%s\n' "$workspace"
      printf 'runtime_image=%s\n' "$runtime"
      printf 'webui_image=%s\n' "$webui"
      printf 'frontend_image=%s\n' "$frontend"
      printf 'recipe_ref=%s/recipe@%s\n' "$prefix" "$digest"
    } >> "$GITHUB_OUTPUT"
    ;;
  attach)
    project="${2:?}"
    installation_env="${3:?}"
    registry_host="${4:?}"
    network="${project}_opl-cloud-local-v2"
    docker network connect "$network" "$registry"
    registry_ip="$(docker inspect "$registry" | jq -er --arg network "$network" '.[0].NetworkSettings.Networks[$network].IPAddress')"
    compose=(docker compose -p "$project" --env-file "$installation_env" -f deploy/portable/compose.local-v2.yaml)
    # Registry references keep the Docker Engine's loopback identity. The Build
    # process reaches that same registry through its own isolated loopback.
    "${compose[@]}" exec -d -T build node -e '
      const net = require("node:net");
      const [host, port] = process.argv.slice(1);
      net.createServer((client) => {
        const upstream = net.connect(Number(port), host);
        client.on("error", () => upstream.destroy());
        upstream.on("error", () => client.destroy());
        client.pipe(upstream).pipe(client);
      }).listen(Number(port), "127.0.0.1");
    ' "$registry_ip" "${registry_host#*:}"
    "${compose[@]}" exec -T build curl --fail --silent --show-error --retry 10 --retry-connrefused "http://$registry_host/v2/"
    "${compose[@]}" exec -T build mkdir -p /var/lib/opl/build/docker
    "${compose[@]}" exec -T build docker --config /var/lib/opl/build/docker buildx create \
      --node qualification --name qualification --driver remote "tcp://$registry_ip:1234"
    "${compose[@]}" exec -T build docker --config /var/lib/opl/build/docker buildx inspect qualification --bootstrap
    ;;
  detach)
    network="${2:?}_opl-cloud-local-v2"
    if docker container inspect "$registry" >/dev/null 2>&1 &&
      docker inspect "$registry" | jq -e --arg network "$network" '.[0].NetworkSettings.Networks | has($network)' >/dev/null; then
      docker network disconnect "$network" "$registry"
    fi
    ;;
  cleanup)
    if docker container inspect "$builder" >/dev/null 2>&1; then
      docker rm --force --volumes "$builder"
    fi
    if docker container inspect "$registry" >/dev/null 2>&1; then
      docker rm --force --volumes "$registry"
    fi
    if [ -n "${2:-}" ]; then
      for repository in qualification-fixtures/runtime qualification-fixtures/webui \
        qualification-fixtures/frontend opl-qualification-workspace; do
        tag="$2/$repository:fixture"
        if docker image inspect "$tag" >/dev/null 2>&1; then
          docker image rm "$tag"
        fi
      done
    fi
    ;;
  *)
    printf 'Expected prepare, attach, detach, or cleanup\n' >&2
    exit 2
    ;;
esac

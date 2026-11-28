# OPL_SOURCE_SHA is the product revision this image is built from. The build
# stamps it into every binary so a running process states its own revision
# instead of only repeating what the deployment claims about it. The Candidate
# workflow passes the same revision it names in the image label, so the two
# cannot disagree.
ARG OPL_SOURCE_SHA

FROM --platform=$BUILDPLATFORM golang:1.27-bookworm@sha256:4fa7c968ba93f1635670522dbb01a2c78150443e1385746924566079f36570ea AS go-build

ARG TARGETOS
ARG TARGETARCH
ARG GOPROXY=https://proxy.golang.org,direct
ARG OPL_SOURCE_SHA
RUN printf '%s' "$OPL_SOURCE_SHA" | grep -Eq '^[0-9a-f]{40}$' || { echo "opl_source_sha_invalid" >&2; exit 1; }

WORKDIR /src
COPY go.mod go.sum /src/
RUN GOPROXY="$GOPROXY" go mod download
COPY packages/contracts/go /src/packages/contracts/go
COPY services /src/services
COPY apps/console-bff /src/apps/console-bff
COPY cmd/opl-cloud /src/cmd/opl-cloud
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH GOPROXY="$GOPROXY" go build -ldflags "-X opl-cloud/packages/contracts/go.ProductSourceSHA=$OPL_SOURCE_SHA" -o /out/opl-cloud ./cmd/opl-cloud \
    && CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH GOPROXY="$GOPROXY" go -C services/fabric build -ldflags "-X opl-cloud/packages/contracts/go.ProductSourceSHA=$OPL_SOURCE_SHA" -o /out/opl-tencent-provisioner ./cmd/opl-tencent-provisioner \
    && CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH GOPROXY="$GOPROXY" go -C services/fabric build -ldflags "-X opl-cloud/packages/contracts/go.ProductSourceSHA=$OPL_SOURCE_SHA" -o /out/opl-node-image-retire ./cmd/opl-node-image-retire \
    && CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH GOPROXY="$GOPROXY" go -C packages/contracts/go build -ldflags "-X opl-cloud/packages/contracts/go.ProductSourceSHA=$OPL_SOURCE_SHA" -o /out/opl-owner-probe ./cmd/opl-owner-probe \
    && ln -s opl-cloud /out/opl-cloud-composition \
    && for bin in opl-capability opl-build opl-runtime-control opl-workspace opl-resource-catalog opl-serve opl-gateway-integration opl-fabric opl-ledger opl-console-bff opl-control-plane; do \
      ln -s opl-cloud "/out/$bin" || exit 1; \
    done

FROM docker:29.10.0-cli@sha256:34507eb5069d8be82c95dd3b6987d4250fd5fee49b82e2509b7d7a7030740d6d AS docker-cli

FROM --platform=$BUILDPLATFORM node:26-bookworm-slim@sha256:367679cf9792759492a486e4aa4b421764d71a9546a6dae8aab81a99eb797b3e AS build

WORKDIR /app
ARG VITE_CONSOLE_IDENTITY=cloud
ENV VITE_CONSOLE_IDENTITY=$VITE_CONSOLE_IDENTITY
COPY package.json package-lock.json ./
RUN npm ci --no-audit --no-fund --fetch-retries=5 --fetch-retry-mintimeout=20000 --fetch-retry-maxtimeout=120000
COPY . .
RUN npm run build

FROM debian:bookworm-slim@sha256:e67eeead736ff7bbe258ef088bff683635c561b1fcfefad0806c1df4ab49ce0e AS runtime

WORKDIR /app
ARG TARGETARCH
# The Console bundle is baked here, so its identity is an image-level fact, not a
# deployment choice: the same digest either carries the cloud identity (the
# Console reaches the Console BFF's /api/v2 surface) or the legacy identity (the
# Console reaches the Control Plane's /api surface). The runtime stage therefore
# declares the same build argument the build stage uses and labels the image with
# it, so an installation, a Local qualification, a Candidate or a Release can read
# which surface this exact digest serves instead of inferring it.
ARG VITE_CONSOLE_IDENTITY=cloud
ENV NODE_ENV=production
ENV CONTROL_PLANE_ADDR=:8787
LABEL opl.cloud.console-identity=$VITE_CONSOLE_IDENTITY

RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates curl \
  && curl -fsSL --http1.1 --retry 5 --retry-all-errors --retry-delay 5 \
       -o /usr/local/bin/kubectl "https://dl.k8s.io/release/v1.30.8/bin/linux/${TARGETARCH}/kubectl" \
  && case "${TARGETARCH}" in \
       amd64) KUBECTL_SHA256="7f39bdcf768ce4b8c1428894c70c49c8b4d2eee52f3606eb02f5f7d10f66d692" ;; \
       arm64) KUBECTL_SHA256="e51d6a76fade0871a9143b64dc62a5ff44f369aa6cb4b04967d93798bf39d15b" ;; \
       *) echo "unsupported TARGETARCH ${TARGETARCH}" >&2; exit 1 ;; \
     esac \
  && echo "${KUBECTL_SHA256}  /usr/local/bin/kubectl" | sha256sum -c \
  && chmod +x /usr/local/bin/kubectl \
  && groupadd --gid 1000 opl \
  && useradd --uid 1000 --gid 1000 --shell /usr/sbin/nologin --create-home opl \
  && rm -rf /var/lib/apt/lists/*

COPY --from=build /app/dist ./dist
COPY --from=go-build /out/ /usr/local/bin/
COPY docs/spec/target/contracts/publisher-contract.schema.json /app/contracts/publisher-contract.schema.json
COPY deploy/portable/opl-cloud-owner-topology.json /app/contracts/opl-cloud-owner-topology.json
COPY --from=docker-cli /usr/local/bin/docker /usr/local/bin/docker
# The Build owner invokes `docker buildx build` for every Agent OCI. The
# docker CLI discovers buildx only as a CLI plugin on its plugin path, so the
# product image must carry the plugin binary or every Agent build fails with
# 'unknown command: buildx'.
COPY --from=docker-cli /usr/local/libexec/docker/cli-plugins/docker-buildx /usr/local/libexec/docker/cli-plugins/docker-buildx
RUN mkdir -p /app/.runtime && chown -R opl:opl /app/.runtime

USER opl
EXPOSE 8787
# The published default command stays the Control Plane process for compatibility
# with existing deployment profiles; the image also ships the composition
# entrypoint and baked owner topology contract (/app/contracts/opl-cloud-owner-topology.json).
CMD ["/usr/local/bin/opl-control-plane"]

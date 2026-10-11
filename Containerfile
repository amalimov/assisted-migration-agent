ARG AGENT_UI_IMAGE=quay.io/redhat-user-workloads/assisted-migration-tenant/migration-planner-agent-ui
ARG AGENT_UI_IMAGE_TAG=latest
FROM --platform=linux/amd64 ${AGENT_UI_IMAGE}:${AGENT_UI_IMAGE_TAG} AS ui-builder

FROM --platform=linux/amd64 registry.access.redhat.com/ubi9/go-toolset AS backend-builder
COPY go.mod go.sum ./
RUN go mod download
USER 0
COPY . .
ARG PLANNER_AGENT_GIT_COMMIT=unknown
ARG PLANNER_AGENT_VERSION=v0.0.0
ARG UI_GIT_COMMIT=unknown
RUN go build -ldflags="-X main.gitCommit=${PLANNER_AGENT_GIT_COMMIT} -X main.version=${PLANNER_AGENT_VERSION} -X main.uiGitCommit=${UI_GIT_COMMIT}" -o /tmp/agent .

FROM --platform=linux/amd64 registry.access.redhat.com/ubi9/ubi-minimal AS opa-builder
RUN microdnf install -y wget tar gzip ca-certificates && microdnf clean all
WORKDIR /app
RUN mkdir -p /app/policies /app/forklift && \
    cd /app/forklift && \
    wget -q https://github.com/kubev2v/forklift/archive/main.tar.gz && \
    tar -xzf main.tar.gz --strip-components=1 && \
    find validation/policies/io/konveyor/forklift/vmware -name '*.rego' ! -name '*_test.rego' -exec cp {} /app/policies/ \;

# The parser executes INSTALL excel; an installed extension in DuckDB's cache
# makes that call work without network access at runtime.
FROM --platform=linux/amd64 registry.access.redhat.com/ubi9/ubi-minimal AS duckdb-extensions
RUN microdnf install -y wget gzip && microdnf clean all
ARG DUCKDB_VERSION=v1.4.3
ARG DUCKDB_EXCEL_SHA256=3a5387ebbb0b8e84fea6eae635bad5f7c9832663d6e44f8a2faf9a315092aee2
WORKDIR /extensions
RUN mkdir -p ${DUCKDB_VERSION}/linux_amd64 && \
    wget -q -O ${DUCKDB_VERSION}/linux_amd64/excel.duckdb_extension.gz "https://extensions.duckdb.org/${DUCKDB_VERSION}/linux_amd64/excel.duckdb_extension.gz" && \
    gunzip ${DUCKDB_VERSION}/linux_amd64/excel.duckdb_extension.gz && \
    echo "${DUCKDB_EXCEL_SHA256}  ${DUCKDB_VERSION}/linux_amd64/excel.duckdb_extension" | sha256sum -c -

FROM --platform=linux/amd64 registry.access.redhat.com/ubi9/ubi-minimal
RUN microdnf install -y ca-certificates tzdata && microdnf clean all
WORKDIR /app
COPY --from=backend-builder /tmp/agent /app/agent
COPY --from=ui-builder /apps/agent-ui/dist /app/static
COPY --from=opa-builder /app/policies /app/policies
COPY --from=duckdb-extensions /extensions /app/extensions
RUN mkdir -p /var/lib/agent && chown -R 1001:0 /var/lib/agent /app/static /app/policies /app/extensions
COPY scripts/entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh
USER 1001
EXPOSE 8000
ENTRYPOINT ["/app/entrypoint.sh"]
CMD ["run", "--data-folder", "/var/lib/agent", "--opa-policies-folder", "/app/policies", "--server-mode", "prod", "--server-statics-folder", "/app/static"]
